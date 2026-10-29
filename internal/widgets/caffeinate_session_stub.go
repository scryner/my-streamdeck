//go:build !darwin || !cgo

package widgets

import "fmt"

type systemCaffeinateLockMonitor struct{}

func (systemCaffeinateLockMonitor) Locked() (bool, error) {
	return false, fmt.Errorf("macOS session monitoring requires Darwin with cgo")
}

func (systemCaffeinateLockMonitor) Observe(func(caffeinateLockEvent)) (func(), error) {
	return func() {}, nil
}
