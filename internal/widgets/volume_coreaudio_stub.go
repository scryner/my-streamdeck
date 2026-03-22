//go:build !darwin

package widgets

import (
	"context"
	"fmt"
)

func readVolumeState(context.Context) (VolumeState, error) {
	return VolumeState{}, fmt.Errorf("Core Audio volume state is only available on macOS")
}

func setOutputVolume(context.Context, int) error {
	return fmt.Errorf("Core Audio output volume is only available on macOS")
}

func setOutputMuted(context.Context, bool) error {
	return fmt.Errorf("Core Audio output mute is only available on macOS")
}

func readOutputSourceName(context.Context) (string, error) {
	return "", fmt.Errorf("Core Audio output source is only available on macOS")
}
