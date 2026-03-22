//go:build !darwin

package widgets

import (
	"context"
	"fmt"
)

func readSystemPlaybackState(context.Context) (playerPlaybackState, error) {
	return playerPlaybackStateUnknown, fmt.Errorf("player control is only available on macOS")
}

func sendSystemPlayPause(context.Context) error {
	return fmt.Errorf("player control is only available on macOS")
}

func sendSystemNextTrack(context.Context) error {
	return fmt.Errorf("player control is only available on macOS")
}

func sendSystemPreviousTrack(context.Context) error {
	return fmt.Errorf("player control is only available on macOS")
}
