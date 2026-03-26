package app

import (
	"errors"
	"testing"
	"time"
)

func TestRuntimeManagerRestartBlocksOnCloseTimeout(t *testing.T) {
	oldTimeout := runtimeCloseTimeout
	oldStartRuntime := startRuntime
	runtimeCloseTimeout = 10 * time.Millisecond
	startCalls := 0
	startRuntime = func() (*Runtime, error) {
		startCalls++
		return nil, errors.New("unexpected start")
	}
	t.Cleanup(func() {
		runtimeCloseTimeout = oldTimeout
		startRuntime = oldStartRuntime
	})

	previous := &Runtime{
		id:               7,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
		unexpectedStopCh: make(chan error),
		startedAt:        time.Now(),
	}
	manager := &runtimeManager{runtime: previous}

	err := manager.restart()
	if !errors.Is(err, ErrRuntimeCloseTimedOut) {
		t.Fatalf("restart() error = %v, want %v", err, ErrRuntimeCloseTimedOut)
	}
	if startCalls != 0 {
		t.Fatalf("startRuntime() called %d times, want 0", startCalls)
	}
	if manager.runtime != previous {
		t.Fatalf("manager.runtime = %p, want previous runtime %p", manager.runtime, previous)
	}
}

func TestRuntimeManagerRestartContinuesAfterCloseWarning(t *testing.T) {
	oldTimeout := runtimeCloseTimeout
	oldStartRuntime := startRuntime
	runtimeCloseTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		runtimeCloseTimeout = oldTimeout
		startRuntime = oldStartRuntime
	})

	previous := &Runtime{
		id:               11,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
		unexpectedStopCh: make(chan error),
		startedAt:        time.Now(),
		closeErr:         errors.New("cleanup warning"),
	}
	close(previous.doneCh)

	nextUnexpectedStop := make(chan error)
	close(nextUnexpectedStop)
	next := &Runtime{
		id:               12,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
		unexpectedStopCh: nextUnexpectedStop,
		startedAt:        time.Now(),
	}

	startCalls := 0
	startRuntime = func() (*Runtime, error) {
		startCalls++
		return next, nil
	}

	manager := &runtimeManager{runtime: previous}
	if err := manager.restart(); err != nil {
		t.Fatalf("restart() error = %v, want nil", err)
	}
	if startCalls != 1 {
		t.Fatalf("startRuntime() called %d times, want 1", startCalls)
	}
	if manager.runtime != next {
		t.Fatalf("manager.runtime = %p, want next runtime %p", manager.runtime, next)
	}
}
