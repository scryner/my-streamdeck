//go:build !darwin

package widgets

import (
	"context"
	"fmt"
)

func readMainDisplayBrightness(context.Context) (int, error) {
	return 0, fmt.Errorf("display brightness is only available on macOS")
}

func setMainDisplayBrightness(context.Context, int) error {
	return fmt.Errorf("display brightness is only available on macOS")
}

func readMainDisplayName(context.Context) (string, error) {
	return "", fmt.Errorf("display brightness is only available on macOS")
}
