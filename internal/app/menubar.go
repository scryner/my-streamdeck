package app

import (
	"context"
	"errors"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

var exitProcess = os.Exit

const (
	runtimeRestartAttempts   = 20
	runtimeRestartRetryDelay = 500 * time.Millisecond
)

var startRuntime = StartRuntime

func RunMenuBar(opts RunOptions) error {
	SetVerboseLogging(opts.Verbose)

	manager := &runtimeManager{}
	var stopWakeObserver sync.Once
	var stopWake func()
	var stopPprof func(context.Context) error

	systray.Run(func() {
		icon := menuBarIcon()
		systray.SetTemplateIcon(icon, icon)
		systray.SetTitle("")
		systray.SetTooltip("my-streamdeck")

		pprofStop, err := startPprofServer(opts.EnablePprof)
		if err != nil {
			log.Printf("start pprof server: %v", err)
		} else {
			stopPprof = pprofStop
		}

		if err := manager.start(); err != nil {
			log.Printf("start runtime: %v", err)
		}

		wakeStop, err := startWakeObserver(func() {
			debugf("wake observer: notification received goroutines=%d", runtime.NumGoroutine())
			if err := manager.restart(); err != nil {
				log.Printf("restart runtime after wake: %v", err)
			}
		})
		if err == nil {
			stopWake = wakeStop
		}

		quitItem := systray.AddMenuItem("Quit", "Quit my-streamdeck")
		go func() {
			<-quitItem.ClickedCh
			stopWakeObserver.Do(func() {
				if stopWake != nil {
					stopWake()
				}
			})
			if stopPprof != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = stopPprof(ctx)
				cancel()
			}
			manager.close()
			systray.Quit()
		}()
	}, func() {
		stopWakeObserver.Do(func() {
			if stopWake != nil {
				stopWake()
			}
		})
		if stopPprof != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = stopPprof(ctx)
			cancel()
		}
		exitProcess(0)
	})

	return nil
}

type runtimeManager struct {
	mu      sync.Mutex
	opMu    sync.Mutex
	runtime *Runtime
	closed  bool
}

func (m *runtimeManager) start() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if m.isClosed() {
		debugf("runtime manager: start skipped because manager is closed")
		return nil
	}

	debugf("runtime manager: start begin goroutines=%d", runtime.NumGoroutine())
	rt, err := startRuntime()
	if err != nil {
		log.Printf("runtime manager: start failed: %v", err)
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		debugf("runtime manager: start produced runtime[%d] but manager is closed; closing runtime", rt.id)
		if closeErr := rt.Close(); closeErr != nil {
			log.Printf("runtime manager: close runtime[%d] after start race: %v", rt.id, closeErr)
		}
		return nil
	}
	m.runtime = rt
	debugf("runtime manager: start attached runtime[%d]", rt.id)
	m.watch(rt)
	return nil
}

func (m *runtimeManager) restart() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if m.isClosed() {
		debugf("runtime manager: restart skipped because manager is closed")
		return nil
	}

	var previous *Runtime
	m.mu.Lock()
	previous = m.runtime
	m.runtime = nil
	m.mu.Unlock()
	if previous != nil {
		debugf("runtime manager: restart begin previous=runtime[%d] goroutines=%d", previous.id, runtime.NumGoroutine())
	} else {
		debugf("runtime manager: restart begin with no active runtime goroutines=%d", runtime.NumGoroutine())
	}

	if previous != nil {
		if closeErr := previous.Close(); closeErr != nil {
			if errors.Is(closeErr, ErrRuntimeCloseTimedOut) {
				m.mu.Lock()
				if !m.closed && m.runtime == nil {
					m.runtime = previous
				}
				m.mu.Unlock()
				log.Printf("runtime manager: restart aborted runtime[%d] did not stop: %v", previous.id, closeErr)
				return closeErr
			}
			log.Printf("runtime manager: runtime[%d] close warning during restart: %v", previous.id, closeErr)
		}
	}

	var lastErr error
	for attempt := 0; attempt < runtimeRestartAttempts; attempt++ {
		if m.isClosed() {
			debugf("runtime manager: restart aborted because manager closed during attempt %d", attempt+1)
			return nil
		}

		debugf("runtime manager: restart attempt=%d goroutines=%d", attempt+1, runtime.NumGoroutine())
		rt, err := startRuntime()
		if err == nil {
			m.mu.Lock()
			if m.closed {
				m.mu.Unlock()
				debugf("runtime manager: restart created runtime[%d] but manager closed; closing runtime", rt.id)
				if closeErr := rt.Close(); closeErr != nil {
					log.Printf("runtime manager: close runtime[%d] after restart race: %v", rt.id, closeErr)
				}
				return nil
			}
			m.runtime = rt
			m.mu.Unlock()
			debugf("runtime manager: restart attached runtime[%d] after attempt=%d", rt.id, attempt+1)
			m.watch(rt)
			return nil
		}

		lastErr = err
		log.Printf("runtime manager: restart attempt=%d failed: %v", attempt+1, err)
		time.Sleep(runtimeRestartRetryDelay)
	}

	log.Printf("runtime manager: restart exhausted attempts lastErr=%v", lastErr)
	return lastErr
}

func (m *runtimeManager) close() {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	m.closed = true
	rt := m.runtime
	m.runtime = nil
	m.mu.Unlock()

	if rt != nil {
		debugf("runtime manager: close runtime[%d]", rt.id)
		if err := rt.Close(); err != nil {
			log.Printf("runtime manager: close runtime[%d] failed: %v", rt.id, err)
		}
	}
}

func (m *runtimeManager) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *runtimeManager) watch(rt *Runtime) {
	go func() {
		debugf("runtime manager: watch begin runtime[%d]", rt.id)
		err, ok := <-rt.UnexpectedStop()
		if !ok || err == nil {
			debugf("runtime manager: watch end runtime[%d] ok=%t err=%v", rt.id, ok, err)
			return
		}

		m.mu.Lock()
		current := m.runtime
		closed := m.closed
		m.mu.Unlock()
		if closed || current != rt {
			currentID := uint64(0)
			if current != nil {
				currentID = current.id
			}
			debugf(
				"runtime manager: watch ignore runtime[%d] current=runtime[%d] closed=%t err=%v",
				rt.id,
				currentID,
				closed,
				err,
			)
			return
		}

		log.Printf("runtime manager: watch runtime[%d] unexpected stop, restarting: %v", rt.id, err)
		if restartErr := m.restart(); restartErr != nil {
			log.Printf("restart runtime after unexpected stop: %v", restartErr)
		}
	}()
}
