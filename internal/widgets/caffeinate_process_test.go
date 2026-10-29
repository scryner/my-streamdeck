package widgets

import (
	"errors"
	"os"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeOwnedCaffeinateProcess struct {
	done       chan struct{}
	killCalled chan struct{}
	killOnce   sync.Once
	mu         sync.Mutex
	killErr    error
	waits      atomic.Int32
}

func newFakeOwnedCaffeinateProcess() *fakeOwnedCaffeinateProcess {
	return &fakeOwnedCaffeinateProcess{done: make(chan struct{}), killCalled: make(chan struct{})}
}
func (p *fakeOwnedCaffeinateProcess) Kill() error {
	p.killOnce.Do(func() { close(p.killCalled) })
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killErr
}
func (p *fakeOwnedCaffeinateProcess) Wait() error {
	p.waits.Add(1)
	<-p.done
	return nil
}

func TestCaffeinateProcessStopWaitsBeforeConcurrentStart(t *testing.T) {
	t.Parallel()
	first, second := newFakeOwnedCaffeinateProcess(), newFakeOwnedCaffeinateProcess()
	b := newCommandCaffeinateBackend()
	var starts atomic.Int32
	b.start = func() (caffeinateProcess, error) {
		if starts.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}
	if err := b.Enable(); err != nil {
		t.Fatal(err)
	}
	stopResult := make(chan error, 1)
	go func() { stopResult <- b.Disable() }()
	<-first.killCalled
	startResult := make(chan error, 1)
	go func() { startResult <- b.Enable() }()
	select {
	case err := <-startResult:
		t.Fatalf("new process started before old Wait completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if starts.Load() != 1 {
		t.Fatal("overlapping processes")
	}
	close(first.done)
	if err := <-stopResult; err != nil {
		t.Fatal(err)
	}
	if err := <-startResult; err != nil {
		t.Fatal(err)
	}
	if !b.Enabled() || starts.Load() != 2 {
		t.Fatal("replacement process missing")
	}
	close(second.done)
	if err := b.Disable(); err != nil {
		t.Fatal(err)
	}
	if first.waits.Load() != 1 || second.waits.Load() != 1 {
		t.Fatal("process not reaped exactly once")
	}
}

func TestCaffeinateProcessExitAndLateNotificationDoNotClearReplacement(t *testing.T) {
	t.Parallel()
	first, second := newFakeOwnedCaffeinateProcess(), newFakeOwnedCaffeinateProcess()
	b := newCommandCaffeinateBackend()
	starts := 0
	b.start = func() (caffeinateProcess, error) {
		starts++
		if starts == 1 {
			return first, nil
		}
		return second, nil
	}
	if err := b.Enable(); err != nil {
		t.Fatal(err)
	}
	close(first.done) // Unexpected exit without Kill.
	waitCaffeinate(t, func() bool { return !b.Enabled() })
	if b.Enabled() {
		t.Fatal("dead child still reported enabled")
	}
	if err := b.Enable(); err != nil {
		t.Fatal(err)
	}
	<-b.Exited() // The old process's notification arrives after replacement.
	if err := b.Enable(); err != nil {
		t.Fatal(err)
	}
	if starts != 2 || !b.Enabled() {
		t.Fatal("duplicate start or stale exit cleared replacement")
	}
	select {
	case <-first.killCalled:
		t.Fatal("killed an already reaped process")
	default:
	}
	close(second.done)
	if err := b.Disable(); err != nil {
		t.Fatal(err)
	}
}

func TestCaffeinateProcessKillFailureRetainsOwnership(t *testing.T) {
	t.Parallel()
	p := newFakeOwnedCaffeinateProcess()
	p.killErr = errors.New("kill failed")
	b := newCommandCaffeinateBackend()
	starts := 0
	b.start = func() (caffeinateProcess, error) { starts++; return p, nil }
	if err := b.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := b.Disable(); err == nil {
		t.Fatal("expected kill error")
	}
	if !b.Enabled() {
		t.Fatal("lost live child after failed Kill")
	}
	if err := b.Enable(); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatal("started over child whose Kill failed")
	}
	close(p.done)
	<-b.Exited()
	if err := b.Disable(); err != nil {
		t.Fatal(err)
	}
}

func TestCaffeinateProcessStartFailureAndOwnerBoundArguments(t *testing.T) {
	t.Parallel()
	b := newCommandCaffeinateBackend()
	b.start = func() (caffeinateProcess, error) { return nil, errors.New("start failed") }
	if err := b.Enable(); err == nil {
		t.Fatal("expected start error")
	}
	if b.Enabled() {
		t.Fatal("failed start reported running")
	}
	if err := b.Disable(); err != nil {
		t.Fatal(err)
	}
	// Construct only: never start the real power assertion executable in tests.
	cmd := caffeinateCommand()
	want := []string{"/usr/bin/caffeinate", "-di", "-w", strconv.Itoa(os.Getpid())}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("command = %v, want %v", cmd.Args, want)
	}
}
