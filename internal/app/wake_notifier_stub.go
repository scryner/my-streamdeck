//go:build !darwin

package app

func startWakeObserver(func()) (func(), error) {
	return func() {}, nil
}
