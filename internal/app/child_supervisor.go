package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"
)

var launchChild = startManagedChild
var terminateChild = stopManagedChild

type childSupervisor struct {
	opts RunOptions

	mu                   sync.Mutex
	opMu                 sync.Mutex
	child                *managedChild
	closed               bool
	checkpointGeneration string // protected by opMu, advanced only after launch succeeds
}

func newChildSupervisor(opts RunOptions) *childSupervisor {
	return &childSupervisor{opts: opts}
}

func (s *childSupervisor) start() error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	if s.isClosed() {
		debugf("child supervisor: start skipped because supervisor is closed")
		return nil
	}
	if s.currentChild() != nil {
		return nil
	}
	return s.startLocked()
}

func (s *childSupervisor) restart() error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	if s.isClosed() {
		debugf("child supervisor: restart skipped because supervisor is closed")
		return nil
	}

	current := s.currentChild()
	if current != nil {
		debugf("child supervisor: restarting child pid=%d", current.pid)
		current.stopRequested.Store(true)
		if err := terminateChild(current, childShutdownTimeout); err != nil {
			return err
		}
		s.clearChild(current)
	}

	return s.startLocked()
}

func (s *childSupervisor) close() {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	s.closed = true
	current := s.child
	s.mu.Unlock()

	if current != nil {
		debugf("child supervisor: stopping child pid=%d", current.pid)
		current.stopRequested.Store(true)
		if err := terminateChild(current, childShutdownTimeout); err != nil {
			log.Printf("child supervisor: stop child pid=%d failed: %v", current.pid, err)
			return
		}
		s.clearChild(current)
	}
}

func (s *childSupervisor) startLocked() error {
	opts := s.opts
	if opts.caffeinateSessionDir != "" {
		var generation [16]byte
		if _, err := rand.Read(generation[:]); err != nil {
			return fmt.Errorf("create worker generation: %w", err)
		}
		opts.caffeinateRestoreGeneration = s.checkpointGeneration
		opts.caffeinateSaveGeneration = hex.EncodeToString(generation[:])
	}
	child, err := launchChild(opts)
	if err != nil {
		return err
	}
	s.checkpointGeneration = opts.caffeinateSaveGeneration

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = terminateChild(child, childShutdownTimeout)
		return nil
	}
	s.child = child
	s.mu.Unlock()

	debugf("child supervisor: started child pid=%d", child.pid)
	s.watch(child)
	return nil
}

func (s *childSupervisor) watch(child *managedChild) {
	go func() {
		<-child.doneCh
		err := child.currentExitErr()

		s.mu.Lock()
		current := s.child
		closed := s.closed
		if current == child {
			s.child = nil
		}
		s.mu.Unlock()

		if closed || current != child {
			debugf("child supervisor: child pid=%d exit ignored current=%t closed=%t err=%v", child.pid, current == child, closed, err)
			return
		}
		if child.stopRequested.Load() {
			debugf("child supervisor: child pid=%d stopped by supervisor err=%v", child.pid, err)
			return
		}

		log.Printf("child supervisor: child pid=%d exited unexpectedly: %v", child.pid, err)
		time.Sleep(childRestartDelay)
		if err := s.start(); err != nil {
			log.Printf("child supervisor: restart child after unexpected exit failed: %v", err)
		}
	}()
}

func (s *childSupervisor) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *childSupervisor) currentChild() *managedChild {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.child
}

// Keep ownership until termination succeeds (or the waiter observes exit).
// In particular, a failed stop must not make the next restart spawn a second
// worker that could write the same saved intent and create another assertion.
func (s *childSupervisor) clearChild(child *managedChild) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.child == child {
		s.child = nil
	}
}
