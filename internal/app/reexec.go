package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var currentExecutable = os.Executable
var currentArgs = func() []string {
	return append([]string(nil), os.Args[1:]...)
}
var currentEnv = os.Environ
var startReplacementProcess = func(exe string, args []string, env []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

func validateReexecTarget() error {
	exe, err := currentExecutable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}

	goBuildFragment := string(filepath.Separator) + "go-build"
	if strings.Contains(exe, goBuildFragment) {
		return fmt.Errorf("reexec-on-wake is not supported with go run; use a built binary")
	}
	return nil
}

func reexecCurrentProcess() error {
	if err := validateReexecTarget(); err != nil {
		return err
	}
	exe, err := currentExecutable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	if err := startReplacementProcess(exe, currentArgs(), currentEnv()); err != nil {
		return fmt.Errorf("start replacement process: %w", err)
	}
	return nil
}
