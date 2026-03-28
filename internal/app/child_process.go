package app

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	childShutdownTimeout = 30 * time.Second
	childRestartDelay    = 500 * time.Millisecond
)

var currentExecutable = os.Executable
var currentEnv = os.Environ

type managedChild struct {
	cmd *exec.Cmd
	pid int

	doneCh chan struct{}

	stopRequested atomic.Bool

	exitErrMu sync.Mutex
	exitErr   error
}

func childProcessArgs(opts RunOptions) []string {
	args := []string{"--spawn"}
	if opts.EnablePprof {
		args = append(args, "--pprof")
	}
	if opts.Verbose {
		args = append(args, "--verbose")
	}
	return args
}

func startManagedChild(opts RunOptions) (*managedChild, error) {
	exe, err := currentExecutable()
	if err != nil {
		return nil, fmt.Errorf("resolve current executable: %w", err)
	}

	cmd := exec.Command(exe, childProcessArgs(opts)...)
	cmd.Env = currentEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start child process: %w", err)
	}

	child := &managedChild{
		cmd:    cmd,
		pid:    cmd.Process.Pid,
		doneCh: make(chan struct{}),
	}

	go func() {
		child.setExitErr(cmd.Wait())
		close(child.doneCh)
	}()

	return child, nil
}

func stopManagedChild(child *managedChild, timeout time.Duration) error {
	if child == nil {
		return nil
	}

	child.stopRequested.Store(true)

	select {
	case <-child.doneCh:
		return nil
	default:
	}

	if err := child.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		select {
		case <-child.doneCh:
			return nil
		default:
			return fmt.Errorf("signal child process: %w", err)
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-child.doneCh:
		return nil
	case <-timer.C:
	}

	if err := child.cmd.Process.Kill(); err != nil {
		select {
		case <-child.doneCh:
			return nil
		default:
			return fmt.Errorf("kill child process: %w", err)
		}
	}

	<-child.doneCh
	return nil
}

func (c *managedChild) setExitErr(err error) {
	c.exitErrMu.Lock()
	defer c.exitErrMu.Unlock()
	c.exitErr = err
}

func (c *managedChild) currentExitErr() error {
	c.exitErrMu.Lock()
	defer c.exitErrMu.Unlock()
	return c.exitErr
}
