package widgets

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const caffeinateLockPollInterval = 250 * time.Millisecond
const caffeinateLockQueryTimeout = 250 * time.Millisecond

var errCaffeinateQueryStale = errors.New("stale session query discarded")

type caffeinateLockQuery struct {
	done             chan struct{}
	revision         uint64
	intentGeneration uint64
	locked           bool
	err              error
	discard          bool // protected by controller.mu; timed-out reads are never reused
}

type caffeinateLockEvent uint64

const (
	caffeinateSessionChanged caffeinateLockEvent = iota
	caffeinateScreenLocked
	caffeinateScreenUnlocked
)

// Notifications are hints. Only a successful current-session read can resume
// assertions; a lock notification can suspend them immediately.
type CaffeinateLockMonitor interface {
	Locked() (bool, error)
	Observe(func(caffeinateLockEvent)) (stop func(), err error)
}

type caffeinateState struct {
	RequestedEnabled bool
	Running          bool
	Locked           bool
	LockKnown        bool
	ActiveDuration   time.Duration
}

func (s caffeinateState) statusText() string {
	switch {
	case !s.RequestedEnabled && s.Running:
		return "OFF / ERROR"
	case !s.RequestedEnabled:
		return "OFF"
	case s.Running && (!s.LockKnown || s.Locked):
		return "ON / ERROR"
	case s.Running:
		return "ON"
	case !s.LockKnown:
		return "ON / WAIT"
	case s.Locked:
		return "ON / LOCK"
	default:
		return "ON / RETRY"
	}
}

type caffeinateController struct {
	// mu serializes intent, queries, process start/stop, and explicit sleep.
	mu               sync.Mutex
	backend          CaffeinateBackend
	monitor          CaffeinateLockMonitor
	store            CaffeinateIntentStore
	now              func() time.Time
	state            caffeinateState
	runAt            time.Time
	closed           bool
	changed          func()
	lastNotified     caffeinateState
	hasNotified      bool
	query            *caffeinateLockQuery
	intentGeneration uint64
	ctx              context.Context
	stop             chan struct{}
	stopOnce         sync.Once

	// Callbacks must never wait on mu: an event can arrive during a query or
	// process start. The revision prevents that older result from resuming us.
	event atomic.Uint64 // sequence in high bits, event kind in low two bits
	wake  chan struct{}

	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	stopObserve func()
	closeErr    error
}

func newCaffeinateController(backend CaffeinateBackend, monitor CaffeinateLockMonitor, store CaffeinateIntentStore, now func() time.Time) *caffeinateController {
	return &caffeinateController{backend: backend, monitor: monitor, store: store, now: now, wake: make(chan struct{}, 1), stop: make(chan struct{})}
}

func (c *caffeinateController) signal(event caffeinateLockEvent) {
	for {
		old := c.event.Load()
		next := old&^3 | uint64(event)
		// Unlock is only a request to query. Repeated unlock notifications
		// cannot invalidate a successful read/start and cause stop/start churn.
		// A lock or wake still invalidates any query already in flight, even
		// when followed immediately by an unlock notification.
		if event != caffeinateScreenUnlocked {
			next = ((old>>2)+1)<<2 | uint64(event)
		}
		if c.event.CompareAndSwap(old, next) {
			break
		}
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *caffeinateController) Start(ctx context.Context) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.stoppingLocked() {
		return errors.New("caffeinate controller is closed")
	}
	if c.cancel != nil {
		return nil
	}
	if c.store != nil {
		intent, err := c.store.Load()
		if err != nil {
			// A bad checkpoint must not put the whole dashboard in a restart
			// loop. Resume OFF, leaving a diagnostic for the failed restoration.
			log.Printf("caffeinate: discard saved intent, starting OFF: %v", err)
			intent = CaffeinateIntent{}
		}
		c.state.RequestedEnabled = intent.RequestedEnabled
		c.state.ActiveDuration = max(0, intent.ActiveDuration)
	}
	stop, err := c.monitor.Observe(c.signal)
	if err != nil {
		// Polling remains authoritative even without notifications.
		log.Printf("caffeinate: observe session (using polling): %v", err)
	} else {
		c.stopObserve = stop
	}
	ctx, c.cancel = context.WithCancel(ctx)
	c.ctx = ctx
	c.done = make(chan struct{})
	if err := c.refreshLocked(); err != nil {
		debugf("caffeinate: initial reconciliation: %v", err)
	}
	go c.run(ctx)
	return nil
}

func (c *caffeinateController) run(ctx context.Context) {
	defer close(c.done)
	defer c.shutdown()
	ticker := time.NewTicker(caffeinateLockPollInterval)
	defer ticker.Stop()
	var exited <-chan struct{}
	if b, ok := c.backend.(interface{ Exited() <-chan struct{} }); ok {
		exited = b.Exited()
	}
	for {
		select {
		case <-c.stop:
			return
		case <-ctx.Done():
			return
		case <-exited:
			// Observe exit now, retry only on the next fresh query, preventing a
			// failing executable from causing a tight respawn loop.
			c.mu.Lock()
			c.syncRunningLocked()
			c.notifyLocked()
			c.mu.Unlock()
		case <-c.wake:
			c.mu.Lock()
			if caffeinateLockEvent(c.event.Load()&3) == caffeinateScreenLocked {
				c.state.LockKnown, c.state.Locked = true, true
				if err := c.reconcileLocked(); err != nil {
					log.Printf("caffeinate: suspend on lock: %v", err)
				}
				c.notifyLocked()
			} else if err := c.refreshLocked(); err != nil {
				debugf("caffeinate: session notification: %v", err)
			}
			c.mu.Unlock()
		case <-ticker.C:
			c.mu.Lock()
			if err := c.refreshLocked(); err != nil {
				debugf("caffeinate: periodic reconciliation: %v", err)
			}
			c.mu.Unlock()
		}
	}
}

func (c *caffeinateController) refreshLocked() error {
	if c.stoppingLocked() {
		return nil
	}
	q := c.query
	if q == nil {
		q = &caffeinateLockQuery{done: make(chan struct{}), revision: c.event.Load() >> 2, intentGeneration: c.intentGeneration}
		c.query = q
		go func() {
			q.locked, q.err = c.monitor.Locked()
			close(q.done)
		}()
	}
	// Native queries cannot be cancelled. Permit only one in flight, release
	// the operation mutex while waiting, and abandon stale results. In
	// particular OFF, a lock notification, and Close must not wait for it.
	var contextDone <-chan struct{}
	if c.ctx != nil {
		contextDone = c.ctx.Done()
	}
	c.mu.Unlock()
	timer := time.NewTimer(caffeinateLockQueryTimeout)
	defer timer.Stop()
	var err error
	completed := false
wait:
	for {
		select {
		case <-q.done:
			completed = true
			break wait
		case <-timer.C:
			err = errors.New("session query timed out")
			break wait
		case <-c.stop:
			err = errors.New("caffeinate controller is stopping")
			break wait
		case <-contextDone:
			err = context.Canceled
			break wait
		case <-c.wake:
			if c.event.Load()>>2 != q.revision {
				err = errCaffeinateQueryStale
				break wait
			}
		}
	}
	c.mu.Lock()
	if !completed {
		select {
		case <-q.done:
			completed = true
		default:
		}
	}
	if c.query != q {
		return nil
	} // another waiter already consumed this read
	if completed {
		c.query = nil
	} else {
		q.discard = true
	}
	if q.discard || q.intentGeneration != c.intentGeneration || c.event.Load()>>2 != q.revision {
		err = errors.Join(err, errCaffeinateQueryStale)
	}
	if completed {
		err = errors.Join(err, q.err)
	}
	if c.stoppingLocked() {
		return err
	}
	c.state.LockKnown = err == nil
	if completed && err == nil {
		c.state.Locked = q.locked
	}
	if c.event.Load()>>2 != q.revision && caffeinateLockEvent(c.event.Load()&3) == caffeinateScreenLocked {
		c.state.LockKnown, c.state.Locked = true, true
	}
	processErr := c.reconcileLocked()
	// An event during Start must also undo that start before releasing mu.
	if c.stoppingLocked() || c.event.Load()>>2 != q.revision {
		c.state.LockKnown = false
		if caffeinateLockEvent(c.event.Load()&3) == caffeinateScreenLocked {
			c.state.LockKnown, c.state.Locked = true, true
		}
		processErr = errors.Join(processErr, c.reconcileLocked())
	}
	c.notifyLocked()
	return errors.Join(err, processErr)
}

func (c *caffeinateController) reconcileLocked() error {
	c.syncRunningLocked() // record an exit before a replacement can hide it
	wantRunning := !c.stoppingLocked() && c.state.RequestedEnabled && c.state.LockKnown && !c.state.Locked
	var err error
	if wantRunning {
		if !c.backend.Enabled() {
			err = c.backend.Enable()
		}
	} else {
		if c.backend.Enabled() {
			err = c.backend.Disable()
		}
	}
	c.syncRunningLocked()
	return err
}

func (c *caffeinateController) syncRunningLocked() {
	running := c.backend.Enabled()
	if running == c.state.Running {
		return
	}
	now := c.now()
	if running {
		c.runAt = now
	} else {
		if c.state.RequestedEnabled {
			c.state.ActiveDuration += max(0, now.Sub(c.runAt))
		}
		c.runAt = time.Time{}
	}
	c.state.Running = running
}

func (c *caffeinateController) State() caffeinateState {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.syncRunningLocked()
	state := c.state
	if state.Running && state.RequestedEnabled {
		state.ActiveDuration += max(0, c.now().Sub(c.runAt))
	}
	return state
}

func (c *caffeinateController) toggle() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.setRequestedLocked(!c.state.RequestedEnabled)
}

func (c *caffeinateController) setRequestedLocked(enabled bool) error {
	if c.stoppingLocked() {
		return errors.New("caffeinate controller is closed")
	}
	c.state.RequestedEnabled = enabled
	c.intentGeneration++
	c.state.ActiveDuration = 0
	c.runAt = c.now()
	var storeErr error
	// ON is checkpointed only at orderly shutdown. A live worker leaves no
	// restorable ON behind, so a failed OFF write followed by a crash cannot
	// revive a previous request. Load consumes the previous checkpoint.
	if !enabled {
		storeErr = c.saveLocked()
	}
	// OFF never depends on a successful query (or a successful state write).
	if !c.state.RequestedEnabled {
		err := c.reconcileLocked()
		c.notifyLocked()
		return errors.Join(storeErr, err)
	}
	return errors.Join(storeErr, c.refreshLocked())
}

func (c *caffeinateController) sleepNow() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Clear intent, including when already paused, before requesting sleep.
	if err := c.setRequestedLocked(false); err != nil {
		return err
	}
	return c.backend.Sleep()
}

func (c *caffeinateController) saveLocked() error {
	if c.store == nil {
		return nil
	}
	return c.store.Save(CaffeinateIntent{RequestedEnabled: c.state.RequestedEnabled, ActiveDuration: c.state.ActiveDuration})
}

func (c *caffeinateController) notifyLocked() {
	if c.hasNotified && c.lastNotified == c.state {
		return
	}
	c.lastNotified, c.hasNotified = c.state, true
	if c.changed != nil {
		c.changed()
	}
}

func (c *caffeinateController) shutdown() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.cleanupLocked()
	c.notifyLocked()
	c.mu.Unlock()
	if c.stopObserve != nil {
		c.stopObserve()
	}
}

func (c *caffeinateController) Close() error {
	c.stopOnce.Do(func() { close(c.stop) })
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	c.mu.Lock()
	wasClosed := c.closed
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		<-c.done
	} else {
		c.shutdown()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if wasClosed && c.closeErr != nil {
		c.cleanupLocked()
	}
	return c.closeErr
}

func (c *caffeinateController) cleanupLocked() {
	c.closeErr = c.backend.Disable()
	c.syncRunningLocked()
	if c.closeErr == nil {
		c.closeErr = c.saveLocked()
	}
}

func (c *caffeinateController) stoppingLocked() bool {
	if c.closed || (c.ctx != nil && c.ctx.Err() != nil) {
		return true
	}
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}
