package cmd

import (
	"fmt"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"github.com/scryner/my-streamdeck/internal/widgets"
	"rafaelmartins.com/p/streamdeck"

	"github.com/spf13/cobra"
)

var clockKey uint8

var clockCmd = &cobra.Command{
	Use:   "clock",
	Short: "Run the clock widget",
	RunE:  runClockWidget,
}

func init() {
	rootCmd.AddCommand(clockCmd)
	clockCmd.Flags().Uint8Var(&clockKey, "key", uint8(streamdeck.KEY_1), "Target key id")
}

func runClockWidget(_ *cobra.Command, _ []string) error {
	switch streamdeck.KeyID(clockKey) {
	case streamdeck.KEY_2:
		return fmt.Errorf("key 2 is reserved for the calendar widget")
	case streamdeck.KEY_3:
		return fmt.Errorf("key 3 is reserved for the sysstat widget")
	}

	device, err := streamdeck.GetDevice("")
	if err != nil {
		return err
	}

	if err := device.Open(); err != nil {
		return err
	}
	defer device.Close()

	widget, err := widgets.NewClockWidget(widgets.ClockWidgetOptions{
		Key:  streamdeck.KeyID(clockKey),
		Size: widgets.DefaultClockWidgetSize,
	})
	if err != nil {
		return err
	}

	calendarWidget, err := widgets.NewCalendarWidget(widgets.CalendarWidgetOptions{
		Key:  streamdeck.KEY_2,
		Size: widgets.DefaultClockWidgetSize,
	})
	if err != nil {
		return err
	}

	sysstatWidget, err := widgets.NewSysstatWidget(widgets.SysstatWidgetOptions{
		Key:  streamdeck.KEY_3,
		Size: widgets.DefaultClockWidgetSize,
	})
	if err != nil {
		return err
	}

	controller := deckbutton.NewController(device)
	defer controller.Close()

	if err := controller.RegisterButtons(widget.Button(), calendarWidget.Button(), sysstatWidget.Button()); err != nil {
		return err
	}

	return device.Listen(nil)
}
