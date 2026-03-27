package app

import (
	"errors"
	"reflect"
	"testing"
)

func TestReexecCurrentProcess(t *testing.T) {
	oldExecutable := currentExecutable
	oldArgs := currentArgs
	oldEnv := currentEnv
	oldStart := startReplacementProcess
	t.Cleanup(func() {
		currentExecutable = oldExecutable
		currentArgs = oldArgs
		currentEnv = oldEnv
		startReplacementProcess = oldStart
	})

	currentExecutable = func() (string, error) {
		return "/tmp/my-streamdeck", nil
	}
	currentArgs = func() []string {
		return []string{"--pprof", "--verbose"}
	}
	currentEnv = func() []string {
		return []string{"A=B"}
	}

	called := false
	startReplacementProcess = func(exe string, args []string, env []string) error {
		called = true
		if exe != "/tmp/my-streamdeck" {
			t.Fatalf("startReplacementProcess() exe = %q, want %q", exe, "/tmp/my-streamdeck")
		}
		if !reflect.DeepEqual(args, []string{"--pprof", "--verbose"}) {
			t.Fatalf("startReplacementProcess() args = %v", args)
		}
		if !reflect.DeepEqual(env, []string{"A=B"}) {
			t.Fatalf("startReplacementProcess() env = %v", env)
		}
		return nil
	}

	if err := reexecCurrentProcess(); err != nil {
		t.Fatalf("reexecCurrentProcess() error = %v, want nil", err)
	}
	if !called {
		t.Fatal("startReplacementProcess() was not called")
	}
}

func TestReexecCurrentProcessExecutableFailure(t *testing.T) {
	oldExecutable := currentExecutable
	t.Cleanup(func() {
		currentExecutable = oldExecutable
	})

	currentExecutable = func() (string, error) {
		return "", errors.New("boom")
	}

	if err := reexecCurrentProcess(); err == nil {
		t.Fatal("reexecCurrentProcess() error = nil, want non-nil")
	}
}

func TestReexecCurrentProcessStartFailure(t *testing.T) {
	oldExecutable := currentExecutable
	oldStart := startReplacementProcess
	t.Cleanup(func() {
		currentExecutable = oldExecutable
		startReplacementProcess = oldStart
	})

	currentExecutable = func() (string, error) {
		return "/tmp/my-streamdeck", nil
	}
	startReplacementProcess = func(string, []string, []string) error {
		return errors.New("start fail")
	}

	if err := reexecCurrentProcess(); err == nil {
		t.Fatal("reexecCurrentProcess() error = nil, want non-nil")
	}
}

func TestValidateReexecTargetRejectsGoRunBinary(t *testing.T) {
	oldExecutable := currentExecutable
	t.Cleanup(func() {
		currentExecutable = oldExecutable
	})

	currentExecutable = func() (string, error) {
		return "/var/folders/zz/tmp/go-build12345/b001/exe/main", nil
	}

	if err := validateReexecTarget(); err == nil {
		t.Fatal("validateReexecTarget() error = nil, want non-nil")
	}
}
