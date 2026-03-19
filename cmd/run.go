package cmd

import (
	"log"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"rafaelmartins.com/p/streamdeck"

	"github.com/spf13/cobra"
)

var (
	runImagePath string
	runFPS       int
	runDuration  time.Duration
	runLoop      bool
	runKey       uint8
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run Stream Deck key listener",
	RunE:  runStreamDeck,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVar(&runImagePath, "image", "", "Path to a GIF or APNG file")
	runCmd.Flags().IntVar(&runFPS, "fps", 15, "Animation frame rate")
	runCmd.Flags().DurationVar(&runDuration, "duration", 0, "Animation duration override; 0 uses the media duration")
	runCmd.Flags().BoolVar(&runLoop, "loop", true, "Loop animation")
	runCmd.Flags().Uint8Var(&runKey, "key", uint8(streamdeck.KEY_1), "Target key id")
	_ = runCmd.MarkFlagRequired("image")
}

func runStreamDeck(_ *cobra.Command, _ []string) error {
	device, err := streamdeck.GetDevice("")
	if err != nil {
		return err
	}

	if err := device.Open(); err != nil {
		return err
	}
	defer device.Close()

	source, err := deckbutton.NewAnimatedImageSource(deckbutton.AnimatedImageSourceOptions{
		Path: runImagePath,
	})
	if err != nil {
		return err
	}

	controller := deckbutton.NewController(device)
	defer controller.Close()

	button := deckbutton.Button{
		Key: streamdeck.KeyID(runKey),
		Animation: &deckbutton.Animation{
			FrameRate: runFPS,
			Duration:  runDuration,
			Loop:      runLoop,
			Source:    source,
		},
		OnPress: func(_ *streamdeck.Device, k *streamdeck.Key) error {
			log.Printf("Key %s pressed!", k)
			duration := k.WaitForRelease()
			log.Printf("Key %s released after %s", k, duration)
			return nil
		},
	}

	if err := controller.RegisterButtons(button); err != nil {
		return err
	}

	return device.Listen(nil)
}
