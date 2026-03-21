package cmd

import (
	"fmt"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"github.com/scryner/my-streamdeck/internal/widgets"
	"rafaelmartins.com/p/streamdeck"

	"github.com/spf13/cobra"
)

var clockKey uint8
var netInterface string

var clockCmd = &cobra.Command{
	Use:   "clock",
	Short: "Run the clock widget",
	RunE:  runClockWidget,
}

func init() {
	rootCmd.AddCommand(clockCmd)
	clockCmd.Flags().Uint8Var(&clockKey, "key", uint8(streamdeck.KEY_1), "Target key id")
	clockCmd.Flags().StringVar(&netInterface, "net-iface", "en0", "Network interface for the netstat widget")
}

func runClockWidget(_ *cobra.Command, _ []string) error {
	switch streamdeck.KeyID(clockKey) {
	case streamdeck.KEY_2:
		return fmt.Errorf("key 2 is reserved for the calendar widget")
	case streamdeck.KEY_3:
		return fmt.Errorf("key 3 is reserved for the sysstat widget")
	case streamdeck.KEY_4:
		return fmt.Errorf("key 4 is reserved for the netstat widget")
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

	netstatWidget, err := widgets.NewNetstatWidget(widgets.NetstatWidgetOptions{
		Key:       streamdeck.KEY_4,
		Size:      widgets.DefaultClockWidgetSize,
		Interface: netInterface,
	})
	if err != nil {
		return err
	}

	controller := deckbutton.NewController(device)
	defer controller.Close()

	if err := controller.RegisterButtons(widget.Button(), calendarWidget.Button(), sysstatWidget.Button(), netstatWidget.Button()); err != nil {
		return err
	}

	return device.Listen(nil)
}
