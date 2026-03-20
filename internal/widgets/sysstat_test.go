package widgets

import (
	"context"
	"image"
	"reflect"
	"testing"

	"rafaelmartins.com/p/streamdeck"
)

func TestSysstatWidgetRendersExpectedBounds(t *testing.T) {
	t.Parallel()

	calls := 0
	widget, err := NewSysstatWidget(SysstatWidgetOptions{
		Key: streamdeck.KEY_3,
		Stats: func(context.Context) (float64, float64, error) {
			calls++
			return 42.6, 67.1, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSysstatWidget: %v", err)
	}

	button := widget.Button()
	if button.Animation == nil {
		t.Fatal("expected sysstat widget to provide animation")
	}
	if button.Animation.FrameRate != sysstatWidgetFrameRate {
		t.Fatalf("expected frame rate %d, got %d", sysstatWidgetFrameRate, button.Animation.FrameRate)
	}

	frame, err := button.Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}

	if !reflect.DeepEqual(frame.Bounds(), image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
		t.Fatalf("unexpected sysstat bounds: %v", frame.Bounds())
	}
	if calls != 1 {
		t.Fatalf("expected stats provider to be called once, got %d", calls)
	}

	upperForegroundPixels := 0
	lowerForegroundPixels := 0
	for y := 0; y < DefaultClockWidgetSize; y++ {
		for x := 0; x < DefaultClockWidgetSize; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if maxUint32(r, g, b) <= 0x9000 {
				continue
			}

			if y < DefaultClockWidgetSize/2 {
				upperForegroundPixels++
				continue
			}
			lowerForegroundPixels++
		}
	}

	if upperForegroundPixels == 0 {
		t.Fatal("expected cpu usage rendering in upper half of sysstat widget")
	}
	if lowerForegroundPixels == 0 {
		t.Fatal("expected memory usage rendering in lower half of sysstat widget")
	}
}

func maxUint32(values ...uint32) uint32 {
	var max uint32
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
