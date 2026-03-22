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

func readInputVolumeState(context.Context) (VolumeState, error) {
	return VolumeState{}, fmt.Errorf("Core Audio input volume state is only available on macOS")
}

func setInputVolume(context.Context, int) error {
	return fmt.Errorf("Core Audio input volume is only available on macOS")
}

func setOutputMuted(context.Context, bool) error {
	return fmt.Errorf("Core Audio output mute is only available on macOS")
}

func setInputMuted(context.Context, bool) error {
	return fmt.Errorf("Core Audio input mute is only available on macOS")
}

func readOutputSourceName(context.Context) (string, error) {
	return "", fmt.Errorf("Core Audio output source is only available on macOS")
}

func readInputSourceName(context.Context) (string, error) {
	return "", fmt.Errorf("Core Audio input source is only available on macOS")
}
