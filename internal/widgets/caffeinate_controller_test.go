package widgets

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

// All session and power operations in these tests are fakes. No test locks the
// Mac, starts caffeinate, or invokes the real sleep command.
type fakeCaffeinateLockMonitor struct {
	mu      sync.Mutex
	locked  bool
	err     error
	handler func(caffeinateLockEvent)
	onRead  func()
	stopped bool
}

func (m *fakeCaffeinateLockMonitor) Locked() (bool, error) {
	m.mu.Lock()
	locked, err, hook := m.locked, m.err, m.onRead
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
	return locked, err
}

func (m *fakeCaffeinateLockMonitor) Observe(fn func(caffeinateLockEvent)) (func(), error) {
	m.mu.Lock()
	m.handler = fn
	m.mu.Unlock()
	return func() { m.mu.Lock(); m.stopped = true; m.mu.Unlock() }, nil
}

func (m *fakeCaffeinateLockMonitor) set(locked bool, err error) {
	m.mu.Lock()
	m.locked, m.err = locked, err
	m.mu.Unlock()
}

func (m *fakeCaffeinateLockMonitor) emit(event caffeinateLockEvent) {
	m.mu.Lock()
	fn := m.handler
	m.mu.Unlock()
	fn(event)
}

type synchronizedCaffeinateBackend struct {
	mu                    sync.Mutex
	running               bool
	starts, stops, sleeps int
	onStart               func()
	sleepErr              error
	exited                chan struct{}
}

func (b *synchronizedCaffeinateBackend) Enable() error {
	b.mu.Lock()
	b.running = true
	b.starts++
	hook := b.onStart
	b.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}
func (b *synchronizedCaffeinateBackend) Disable() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running {
		b.stops++
	}
	b.running = false
	return nil
}
func (b *synchronizedCaffeinateBackend) Enabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}
func (b *synchronizedCaffeinateBackend) Sleep() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sleeps++
	return b.sleepErr
}
func (b *synchronizedCaffeinateBackend) Exited() <-chan struct{} { return b.exited }
func (b *synchronizedCaffeinateBackend) calls() [3]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return [3]int{b.starts, b.stops, b.sleeps}
}

func refreshCaffeinate(c *caffeinateController) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refreshLocked()
}

func requireCaffeinateState(t *testing.T, c *caffeinateController, requested, running bool) caffeinateState {
	t.Helper()
	state := c.State()
	if state.RequestedEnabled != requested || state.Running != running {
		t.Fatalf("state = %+v, want requested=%t running=%t", state, requested, running)
	}
	return state
}

func waitCaffeinate(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for caffeinate state")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCaffeinateLockTransitionsAndActiveTimer(t *testing.T) {
	t.Parallel()
	backend := &synchronizedCaffeinateBackend{}
	monitor := &fakeCaffeinateLockMonitor{}
	now := time.Unix(1000, 0)
	c := newCaffeinateController(backend, monitor, nil, func() time.Time { return now })
	t.Cleanup(func() { _ = c.Close() })
	if err := c.toggle(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	monitor.set(true, nil)
	for range 3 {
		if err := refreshCaffeinate(c); err != nil {
			t.Fatal(err)
		}
	}
	state := requireCaffeinateState(t, c, true, false)
	if state.ActiveDuration != 10*time.Second || state.statusText() != "ON / LOCK" {
		t.Fatalf("paused state = %+v", state)
	}
	now = now.Add(time.Hour)
	if c.State().ActiveDuration != 10*time.Second {
		t.Fatal("paused time counted as active")
	}
	monitor.set(false, nil)
	for range 3 {
		if err := refreshCaffeinate(c); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(5 * time.Second)
	state = requireCaffeinateState(t, c, true, true)
	if state.ActiveDuration != 15*time.Second {
		t.Fatalf("active = %s", state.ActiveDuration)
	}
	if got := backend.calls(); got != [3]int{2, 1, 0} {
		t.Fatalf("duplicate reads changed processes: %v", got)
	}
	if err := c.toggle(); err != nil {
		t.Fatal(err)
	}
	if c.State().ActiveDuration != 0 {
		t.Fatal("OFF must reset timer")
	}
}

func TestCaffeinateOffWhileLockedDoesNotResume(t *testing.T) {
	t.Parallel()
	for _, initiallyLocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "lock after ON", true: "ON while locked"}[initiallyLocked], func(t *testing.T) {
			b := &synchronizedCaffeinateBackend{}
			m := &fakeCaffeinateLockMonitor{locked: initiallyLocked}
			c := newCaffeinateController(b, m, nil, time.Now)
			defer c.Close()
			if err := c.toggle(); err != nil {
				t.Fatal(err)
			}
			m.set(true, nil)
			if err := refreshCaffeinate(c); err != nil {
				t.Fatal(err)
			}
			requireCaffeinateState(t, c, true, false)
			if err := c.toggle(); err != nil {
				t.Fatal(err)
			}
			m.set(false, nil)
			if err := refreshCaffeinate(c); err != nil {
				t.Fatal(err)
			}
			requireCaffeinateState(t, c, false, false)
		})
	}
}

func TestCaffeinateFailedQuerySuspendsAndNeverMeansUnlocked(t *testing.T) {
	t.Parallel()
	b := &synchronizedCaffeinateBackend{}
	m := &fakeCaffeinateLockMonitor{err: errors.New("no session")}
	c := newCaffeinateController(b, m, nil, time.Now)
	defer c.Close()
	if err := c.toggle(); err == nil {
		t.Fatal("expected query failure")
	}
	if state := requireCaffeinateState(t, c, true, false); state.LockKnown || state.statusText() != "ON / WAIT" {
		t.Fatalf("failed initial query = %+v", state)
	}
	m.set(false, nil)
	if err := refreshCaffeinate(c); err != nil {
		t.Fatal(err)
	}
	requireCaffeinateState(t, c, true, true)
	m.set(false, errors.New("read failed"))
	if err := refreshCaffeinate(c); err == nil {
		t.Fatal("expected query failure")
	}
	requireCaffeinateState(t, c, true, false)
	// An OFF request must still succeed when queries are unavailable.
	if err := c.toggle(); err != nil {
		t.Fatal(err)
	}
	m.set(false, nil)
	if err := refreshCaffeinate(c); err != nil {
		t.Fatal(err)
	}
	requireCaffeinateState(t, c, false, false)
}

func TestCaffeinateDiscardsQueriesAndStartsOvertakenByLock(t *testing.T) {
	t.Parallel()
	for _, duringStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "query", true: "process start"}[duringStart], func(t *testing.T) {
			b := &synchronizedCaffeinateBackend{}
			m := &fakeCaffeinateLockMonitor{}
			c := newCaffeinateController(b, m, nil, time.Now)
			defer c.Close()
			hook := func() { c.signal(caffeinateScreenLocked); m.set(true, nil) }
			if duringStart {
				b.onStart = hook
			} else {
				m.onRead = hook
			}
			if err := c.toggle(); err != nil {
				t.Fatal(err)
			}
			requireCaffeinateState(t, c, true, false)
			want := [3]int{}
			if duringStart {
				want = [3]int{1, 1, 0}
			}
			if got := b.calls(); got != want {
				t.Fatalf("calls = %v, want %v", got, want)
			}
		})
	}
}

func TestCaffeinateLockThenUnlockCannotValidateAnOlderQuery(t *testing.T) {
	t.Parallel()
	b := &synchronizedCaffeinateBackend{}
	m := &fakeCaffeinateLockMonitor{}
	c := newCaffeinateController(b, m, nil, time.Now)
	defer c.Close()
	m.onRead = func() {
		c.signal(caffeinateScreenLocked)
		c.signal(caffeinateScreenUnlocked)
	}
	if err := c.toggle(); err != nil {
		t.Fatal(err)
	}
	if s := requireCaffeinateState(t, c, true, false); s.LockKnown {
		t.Fatal("older query survived a lock/unlock burst")
	}
	m.onRead = nil
	if err := refreshCaffeinate(c); err != nil {
		t.Fatal(err)
	}
	requireCaffeinateState(t, c, true, true)
}

func TestCaffeinatePollingRepairsMissedEventsAndCancellationCleansUp(t *testing.T) {
	t.Parallel()
	b := &synchronizedCaffeinateBackend{}
	m := &fakeCaffeinateLockMonitor{}
	c := newCaffeinateController(b, m, nil, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer c.Close()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.toggle(); err != nil {
		t.Fatal(err)
	}
	m.set(true, nil) // Deliberately no notification.
	waitCaffeinate(t, func() bool { return !c.State().Running })
	m.set(false, nil)
	waitCaffeinate(t, func() bool { return c.State().Running })
	cancel()
	waitCaffeinate(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.stopped })
	requireCaffeinateState(t, c, true, false)
	// A callback already copied by the native layer may arrive after removal.
	m.emit(caffeinateSessionChanged)
	if err := c.toggle(); err == nil {
		t.Fatal("closed controller accepted input")
	}
	requireCaffeinateState(t, c, true, false)
}

func TestCaffeinateDuplicateAndRapidNotifications(t *testing.T) {
	t.Parallel()
	b := &synchronizedCaffeinateBackend{}
	m := &fakeCaffeinateLockMonitor{}
	c := newCaffeinateController(b, m, nil, time.Now)
	defer c.Close()
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.toggle(); err != nil {
		t.Fatal(err)
	}
	m.set(true, nil)
	for range 100 {
		m.emit(caffeinateScreenLocked)
	}
	waitCaffeinate(t, func() bool { return !c.State().Running })
	m.set(false, nil)
	for range 100 {
		m.emit(caffeinateScreenUnlocked)
	}
	waitCaffeinate(t, func() bool { return c.State().Running })
	if got := b.calls(); got != [3]int{2, 1, 0} {
		t.Fatalf("duplicate events: calls = %v", got)
	}
	// Coalesced, rapid transitions must converge to the latest real state.
	m.set(true, nil)
	m.emit(caffeinateScreenLocked)
	m.set(false, nil)
	m.emit(caffeinateScreenUnlocked)
	m.set(true, nil)
	m.emit(caffeinateScreenLocked)
	waitCaffeinate(t, func() bool { s := c.State(); return s.LockKnown && s.Locked && !s.Running })
}

func TestCaffeinateRestartWhileLockedAndLongPressSleepClearsIntent(t *testing.T) {
	t.Parallel()
	store := NewCaffeinateSessionStore(t.TempDir(), "key7")
	if err := store.Save(CaffeinateIntent{RequestedEnabled: true, ActiveDuration: 12 * time.Second}); err != nil {
		t.Fatal(err)
	}
	b := &synchronizedCaffeinateBackend{sleepErr: errors.New("fake sleep failed")}
	m := &fakeCaffeinateLockMonitor{locked: true}
	c := newCaffeinateController(b, m, store, time.Now)
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := requireCaffeinateState(t, c, true, false)
	if s.ActiveDuration != 12*time.Second || b.calls()[0] != 0 {
		t.Fatalf("locked startup = %+v", s)
	}
	if err := c.sleepNow(); err == nil {
		t.Fatal("expected injected sleep error")
	}
	requireCaffeinateState(t, c, false, false)
	if got := b.calls(); got != [3]int{0, 0, 1} {
		t.Fatalf("sleep calls = %v", got)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate the menu bar's worker replacement after wake. Even a failed
	// explicit sleep cleared the saved intent before the sleep command.
	next := newCaffeinateController(&synchronizedCaffeinateBackend{}, &fakeCaffeinateLockMonitor{}, store, time.Now)
	defer next.Close()
	if err := next.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireCaffeinateState(t, next, false, false)
	if next.State().ActiveDuration != 0 {
		t.Fatal("sleep did not reset timer")
	}
}

func TestCaffeinateWakeReplacementPreservesIntentAndPausedTime(t *testing.T) {
	t.Parallel()
	store := NewCaffeinateSessionStore(t.TempDir(), "key7")
	now := time.Unix(1000, 0)
	m := &fakeCaffeinateLockMonitor{}
	c := newCaffeinateController(&synchronizedCaffeinateBackend{}, m, store, func() time.Time { return now })
	// No background loop is needed to exercise deterministic checkpointing.
	if err := c.toggle(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	m.set(true, nil)
	if err := refreshCaffeinate(c); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	next := newCaffeinateController(&synchronizedCaffeinateBackend{}, &fakeCaffeinateLockMonitor{}, store, time.Now)
	defer next.Close()
	if err := next.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := requireCaffeinateState(t, next, true, true)
	if s.ActiveDuration < 10*time.Second || s.ActiveDuration > 11*time.Second {
		t.Fatalf("restored duration = %s", s.ActiveDuration)
	}
}

func TestCaffeinateUnexpectedExitRetriesAfterFreshLockQuery(t *testing.T) {
	t.Parallel()
	b := &synchronizedCaffeinateBackend{exited: make(chan struct{}, 1)}
	m := &fakeCaffeinateLockMonitor{}
	c := newCaffeinateController(b, m, nil, time.Now)
	defer c.Close()
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.toggle(); err != nil {
		t.Fatal(err)
	}
	m.set(true, nil)
	b.mu.Lock()
	b.running = false
	b.mu.Unlock()
	b.exited <- struct{}{}
	waitCaffeinate(t, func() bool { return !c.State().Running })
	if err := refreshCaffeinate(c); err != nil {
		t.Fatal(err)
	}
	if b.calls()[0] != 1 {
		t.Fatal("unexpected exit restarted while locked")
	}
	m.set(false, nil)
	if err := refreshCaffeinate(c); err != nil {
		t.Fatal(err)
	}
	requireCaffeinateState(t, c, true, true)
}

func TestCaffeinateSessionFlags(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                 string
		available            bool
		console, login, lock int
		locked, fail         bool
	}{
		{"unlocked key absent", true, 1, 1, -1, false, false},
		{"unlocked explicit", true, 1, 1, 0, false, false},
		{"locked", true, 1, 1, 1, true, false},
		{"no dictionary", false, 0, 0, 0, false, true},
		{"malformed lock", true, 1, 1, -2, false, true},
		{"missing console", true, -1, 1, -1, false, true},
		{"logged out", true, 1, 0, -1, false, true},
		{"other console user", true, 0, 1, -1, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			locked, err := caffeinateSessionLocked(tt.available, tt.console, tt.login, tt.lock)
			if locked != tt.locked || (err != nil) != tt.fail {
				t.Fatalf("got (%t,%v)", locked, err)
			}
		})
	}
}

func TestCaffeinateIntentStoreRejectsCorruption(t *testing.T) {
	t.Parallel()
	store := NewCaffeinateSessionStore(t.TempDir(), "key7")
	want := CaffeinateIntent{RequestedEnabled: true, ActiveDuration: 25 * time.Second}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	if err := os.WriteFile(store.(fileCaffeinateIntentStore).path, []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	b := &synchronizedCaffeinateBackend{}
	c := newCaffeinateController(b, &fakeCaffeinateLockMonitor{}, store, time.Now)
	if err := c.Start(context.Background()); err == nil {
		t.Fatal("corrupt saved intent accepted")
	}
	if b.Enabled() {
		t.Fatal("corrupt intent started assertion")
	}
}
