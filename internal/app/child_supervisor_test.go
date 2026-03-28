package app

import (
	"errors"
	"testing"
	"time"
)

func TestChildSupervisorRestartStopsAndStartsChild(t *testing.T) {
	oldLaunchChild := launchChild
	oldTerminateChild := terminateChild
	t.Cleanup(func() {
		launchChild = oldLaunchChild
		terminateChild = oldTerminateChild
	})

	startCalls := 0
	stopCalls := 0
	first := &managedChild{pid: 101, doneCh: make(chan struct{})}
	second := &managedChild{pid: 102, doneCh: make(chan struct{})}
	close(first.doneCh)

	launchChild = func(RunOptions) (*managedChild, error) {
		startCalls++
		if startCalls == 1 {
			return first, nil
		}
		return second, nil
	}
	terminateChild = func(child *managedChild, _ time.Duration) error {
		stopCalls++
		if child != first {
			t.Fatalf("terminateChild() child = %p, want %p", child, first)
		}
		return nil
	}

	supervisor := newChildSupervisor(RunOptions{})
	if err := supervisor.start(); err != nil {
		t.Fatalf("start() error = %v, want nil", err)
	}
	if err := supervisor.restart(); err != nil {
		t.Fatalf("restart() error = %v, want nil", err)
	}
	if startCalls != 2 {
		t.Fatalf("launchChild() called %d times, want 2", startCalls)
	}
	if stopCalls != 1 {
		t.Fatalf("terminateChild() called %d times, want 1", stopCalls)
	}
	if supervisor.child != second {
		t.Fatalf("supervisor.child = %p, want %p", supervisor.child, second)
	}
}

func TestChildSupervisorRestartReturnsStopError(t *testing.T) {
	oldTerminateChild := terminateChild
	t.Cleanup(func() {
		terminateChild = oldTerminateChild
	})

	expected := errors.New("stop failed")
	child := &managedChild{pid: 101, doneCh: make(chan struct{})}
	supervisor := newChildSupervisor(RunOptions{})
	supervisor.child = child

	terminateChild = func(*managedChild, time.Duration) error {
		return expected
	}

	if err := supervisor.restart(); !errors.Is(err, expected) {
		t.Fatalf("restart() error = %v, want %v", err, expected)
	}
}
