package app

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

var exitProcess = os.Exit

const runtimeRestartAttempts = 20

func RunMenuBar() error {
	manager := &runtimeManager{}
	var stopWakeObserver sync.Once
	var stopWake func()

	systray.Run(func() {
		icon := menuBarIcon()
		systray.SetTemplateIcon(icon, icon)
		systray.SetTitle("")
		systray.SetTooltip("my-streamdeck")

		if err := manager.start(); err != nil {
			log.Printf("start runtime: %v", err)
		}

		stop, err := startWakeObserver(func() {
			if err := manager.restart(); err != nil {
				log.Printf("restart runtime after wake: %v", err)
			}
		})
		if err == nil {
			stopWake = stop
		}

		quitItem := systray.AddMenuItem("Quit", "Quit my-streamdeck")
		go func() {
			<-quitItem.ClickedCh
			stopWakeObserver.Do(func() {
				if stopWake != nil {
					stopWake()
				}
			})
			manager.close()
			systray.Quit()
		}()
	}, func() {
		stopWakeObserver.Do(func() {
			if stopWake != nil {
				stopWake()
			}
		})
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
		return nil
	}

	rt, err := StartRuntime()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		rt.Close()
		return nil
	}
	m.runtime = rt
	m.watch(rt)
	return nil
}

func (m *runtimeManager) restart() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if m.isClosed() {
		return nil
	}

	var previous *Runtime
	m.mu.Lock()
	previous = m.runtime
	m.runtime = nil
	m.mu.Unlock()

	if previous != nil {
		previous.Close()
	}

	var lastErr error
	for attempt := 0; attempt < runtimeRestartAttempts; attempt++ {
		if m.isClosed() {
			return nil
		}

		rt, err := StartRuntime()
		if err == nil {
			m.mu.Lock()
			if m.closed {
				m.mu.Unlock()
				rt.Close()
				return nil
			}
			m.runtime = rt
			m.mu.Unlock()
			m.watch(rt)
			return nil
		}

		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}

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
		rt.Close()
	}
}

func (m *runtimeManager) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *runtimeManager) watch(rt *Runtime) {
	go func() {
		err, ok := <-rt.UnexpectedStop()
		if !ok || err == nil {
			return
		}

		m.mu.Lock()
		current := m.runtime
		closed := m.closed
		m.mu.Unlock()
		if closed || current != rt {
			return
		}

		log.Printf("runtime stopped unexpectedly, attempting restart: %v", err)
		if restartErr := m.restart(); restartErr != nil {
			log.Printf("restart runtime after unexpected stop: %v", restartErr)
		}
	}()
}
