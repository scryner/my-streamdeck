package widgets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

type caffeinateProcess interface {
	Kill() error
	Wait() error
}

type execCaffeinateProcess struct{ cmd *exec.Cmd }

func (p execCaffeinateProcess) Kill() error { return p.cmd.Process.Kill() }
func (p execCaffeinateProcess) Wait() error { return p.cmd.Wait() }

type commandCaffeinateBackend struct {
	mu      sync.Mutex
	process caffeinateProcess
	done    chan struct{}
	exited  chan struct{}
	start   func() (caffeinateProcess, error)
}

func newCommandCaffeinateBackend() *commandCaffeinateBackend {
	return &commandCaffeinateBackend{exited: make(chan struct{}, 1), start: startCaffeinateProcess}
}

func caffeinateCommand() *exec.Cmd {
	// -w releases our assertions even if the worker is killed without cleanup.
	return exec.Command("/usr/bin/caffeinate", "-di", "-w", strconv.Itoa(os.Getpid()))
}

func startCaffeinateProcess() (caffeinateProcess, error) {
	cmd := caffeinateCommand()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return execCaffeinateProcess{cmd}, nil
}

func (b *commandCaffeinateBackend) Enable() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.runningLocked() {
		return nil
	}
	process, err := b.start()
	if err != nil {
		return fmt.Errorf("start caffeinate: %w", err)
	}
	b.process = process
	done := make(chan struct{})
	b.done = done
	go func() {
		_ = process.Wait() // Exactly one waiter, including unexpected exits.
		close(done)        // No backend mutex needed: Disable may be waiting on it.
		select {
		case b.exited <- struct{}{}:
		default:
		}
	}()
	return nil
}

func (b *commandCaffeinateBackend) runningLocked() bool {
	if b.process == nil {
		return false
	}
	select {
	case <-b.done:
		return false
	default:
		return true
	}
}

func (b *commandCaffeinateBackend) Disable() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.runningLocked() {
		if err := b.process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("stop caffeinate: %w", err)
		}
		// Keep the handle AND the mutex until Wait completes. A concurrent Enable
		// cannot create overlapping assertions or lose the process being stopped.
		<-b.done
	}
	b.process, b.done = nil, nil
	return nil
}

func (b *commandCaffeinateBackend) Enabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.runningLocked()
}

func (b *commandCaffeinateBackend) Exited() <-chan struct{} { return b.exited }

func (b *commandCaffeinateBackend) Sleep() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", `tell application "System Events" to sleep`)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sleep system: %w", err)
	}
	return nil
}
