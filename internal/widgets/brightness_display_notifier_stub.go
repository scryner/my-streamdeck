//go:build !darwin

package widgets

func startDisplayObserver(func()) (func(), error) {
	return func() {}, nil
}
