package app

import (
	"errors"
	"sync"
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
		close(child.doneCh)
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
	second.stopRequested.Store(true)
	close(second.doneCh)
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
	if supervisor.currentChild() != child {
		t.Fatal("failed stop discarded ownership of the live worker")
	}
}

func TestChildSupervisorFailedStopBlocksReplacementUntilRetrySucceeds(t *testing.T) {
	oldLaunch, oldTerminate := launchChild, terminateChild
	t.Cleanup(func() { launchChild, terminateChild = oldLaunch, oldTerminate })
	first := &managedChild{pid: 101, doneCh: make(chan struct{})}
	second := &managedChild{pid: 102, doneCh: make(chan struct{})}
	s := newChildSupervisor(RunOptions{})
	s.child = first
	starts, stops := 0, 0
	launchChild = func(RunOptions) (*managedChild, error) { starts++; return second, nil }
	stopErr := errors.New("stop failed")
	terminateChild = func(child *managedChild, _ time.Duration) error {
		stops++
		if child != first {
			t.Fatalf("stopped %p, want original worker %p", child, first)
		}
		if stops == 1 {
			return stopErr
		}
		close(child.doneCh)
		return nil
	}
	if err := s.restart(); !errors.Is(err, stopErr) {
		t.Fatalf("first restart: %v", err)
	}
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if starts != 0 {
		t.Fatal("start replaced a worker whose termination failed")
	}
	if err := s.restart(); err != nil {
		t.Fatal(err)
	}
	if stops != 2 || starts != 1 || s.currentChild() != second {
		t.Fatalf("retry stops=%d starts=%d child=%p", stops, starts, s.currentChild())
	}
	second.stopRequested.Store(true)
	close(second.doneCh)
}

func TestChildSupervisorCloseRetainsFailedStopForRetry(t *testing.T) {
	oldTerminate := terminateChild
	t.Cleanup(func() { terminateChild = oldTerminate })
	child := &managedChild{pid: 101, doneCh: make(chan struct{})}
	s := newChildSupervisor(RunOptions{})
	s.child = child
	stops := 0
	terminateChild = func(got *managedChild, _ time.Duration) error {
		if got != child {
			t.Fatal("lost original child")
		}
		stops++
		if stops == 1 {
			return errors.New("stop failed")
		}
		close(child.doneCh)
		return nil
	}
	s.close()
	if !s.isClosed() || s.currentChild() != child {
		t.Fatal("failed close lost child ownership")
	}
	s.close()
	if stops != 2 || s.currentChild() != nil {
		t.Fatal("close did not retry owned child")
	}
}

func TestChildSupervisorStartConcurrentWithExit(t *testing.T) {
	oldLaunch := launchChild
	t.Cleanup(func() { launchChild = oldLaunch })
	var children []*managedChild
	launchChild = func(RunOptions) (*managedChild, error) {
		child := &managedChild{pid: len(children) + 1, doneCh: make(chan struct{})}
		children = append(children, child)
		return child, nil
	}
	s := newChildSupervisor(RunOptions{})
	// An exited worker's watcher writes s.child concurrently with explicit
	// start requests. All reads must use the same mutex as that watcher.
	for range 100 {
		if err := s.start(); err != nil {
			t.Fatal(err)
		}
		child := s.currentChild()
		child.stopRequested.Store(true)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_ = s.start()
			}
		}()
		close(child.doneCh)
		wg.Wait()
		// Remove only this completed child if the asynchronous watcher has
		// not run yet; never overwrite a replacement created by start.
		s.clearChild(child)
	}
	if child := s.currentChild(); child != nil {
		child.stopRequested.Store(true)
		close(child.doneCh)
	}
}

func TestChildSupervisorPreservesCaffeinateSessionAcrossWakeRestart(t *testing.T) {
	oldLaunch, oldTerminate := launchChild, terminateChild
	t.Cleanup(func() { launchChild, terminateChild = oldLaunch, oldTerminate })
	directory := t.TempDir()
	var children []*managedChild
	var launches []RunOptions
	failNext := false
	launchChild = func(opts RunOptions) (*managedChild, error) {
		launches = append(launches, opts)
		if failNext {
			failNext = false
			return nil, errors.New("launch failed")
		}
		if opts.caffeinateSessionDir != directory {
			t.Errorf("lost caffeinate session on launch: %q", opts.caffeinateSessionDir)
		}
		child := &managedChild{pid: len(children) + 1, doneCh: make(chan struct{})}
		children = append(children, child)
		return child, nil
	}
	terminateChild = func(child *managedChild, _ time.Duration) error {
		child.stopRequested.Store(true)
		close(child.doneCh)
		return nil
	}
	s := newChildSupervisor(RunOptions{caffeinateSessionDir: directory})
	defer s.close()
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	failNext = true
	if err := s.restart(); err == nil {
		t.Fatal("expected launch failure")
	}
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if err := s.restart(); err != nil {
		t.Fatal(err)
	}
	if len(children) != 3 {
		t.Fatalf("launches = %d", len(children))
	}
	if launches[0].caffeinateRestoreGeneration != "" || launches[0].caffeinateSaveGeneration == "" {
		t.Fatal("first worker did not receive a fresh generation")
	}
	first := launches[0].caffeinateSaveGeneration
	if launches[1].caffeinateRestoreGeneration != first || launches[2].caffeinateRestoreGeneration != first {
		t.Fatal("failed launch advanced the checkpoint generation")
	}
	if launches[3].caffeinateRestoreGeneration != launches[2].caffeinateSaveGeneration || launches[3].caffeinateSaveGeneration == launches[2].caffeinateSaveGeneration {
		t.Fatal("restart did not restore only the immediate predecessor")
	}
}
