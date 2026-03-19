package cmd

import (
	"image/color"
	"log"

	"rafaelmartins.com/p/streamdeck"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run Stream Deck key listener",
	RunE:  runStreamDeck,
}

func init() {
	rootCmd.AddCommand(runCmd)
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

	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	if err := device.SetKeyColor(streamdeck.KEY_1, red); err != nil {
		return err
	}

	if err := device.AddKeyHandler(streamdeck.KEY_1, func(_ *streamdeck.Device, k *streamdeck.Key) error {
		log.Printf("Key %s pressed!", k)
		duration := k.WaitForRelease()
		log.Printf("Key %s released after %s", k, duration)
		return nil
	}); err != nil {
		return err
	}

	return device.Listen(nil)
}
