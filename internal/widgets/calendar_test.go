package widgets

import (
	"context"
	"image"
	"reflect"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

func TestCalendarWidgetRendersExpectedBounds(t *testing.T) {
	t.Parallel()

	widget, err := NewCalendarWidget(CalendarWidgetOptions{
		Key: streamdeck.KEY_2,
		Now: func() time.Time { return time.Date(2026, time.March, 24, 10, 15, 30, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewCalendarWidget: %v", err)
	}

	button := widget.Button()
	if button.Animation == nil {
		t.Fatal("expected calendar widget to provide animation")
	}
	if button.Animation.FrameRate != calendarWidgetFrameRate {
		t.Fatalf("expected frame rate %d, got %d", calendarWidgetFrameRate, button.Animation.FrameRate)
	}

	frame, err := button.Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}

	if !reflect.DeepEqual(frame.Bounds(), image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
		t.Fatalf("unexpected calendar bounds: %v", frame.Bounds())
	}

	brightPixels := 0
	for y := DefaultClockWidgetSize / 2; y < DefaultClockWidgetSize; y++ {
		for x := 0; x < DefaultClockWidgetSize; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if r > 0xd000 && g > 0xd000 && b > 0xd000 {
				brightPixels++
			}
		}
	}

	if brightPixels == 0 {
		t.Fatal("expected bright date pixels in lower half of the calendar widget")
	}
}

func TestCalendarWidgetButtonPressOpensApp(t *testing.T) {
	t.Parallel()

	opened := false
	widget, err := NewCalendarWidget(CalendarWidgetOptions{
		Key: streamdeck.KEY_2,
		OpenApp: func(context.Context) error {
			opened = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewCalendarWidget: %v", err)
	}

	button := widget.Button()
	if button.OnPress == nil {
		t.Fatal("expected calendar widget to provide press handler")
	}

	if err := button.OnPress(nil, nil); err != nil {
		t.Fatalf("OnPress: %v", err)
	}
	if !opened {
		t.Fatal("expected calendar widget press handler to open the calendar app")
	}
}
