//go:build !darwin

package widgets

func startVolumeObserver(func()) (func(), error) {
	return func() {}, nil
}
