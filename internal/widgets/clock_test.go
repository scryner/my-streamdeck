package widgets

import (
	"context"
	"image"
	"reflect"
	"sync"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

func TestClockWidgetStartsInAnalogMode(t *testing.T) {
	t.Parallel()

	widget, err := NewClockWidget(ClockWidgetOptions{
		Key: streamdeck.KEY_1,
		Now: func() time.Time { return time.Date(2026, time.March, 20, 10, 15, 30, 250_000_000, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewClockWidget: %v", err)
	}

	if widget.Mode() != ClockModeAnalog {
		t.Fatalf("expected initial mode analog, got %s", widget.Mode())
	}

	button := widget.Button()
	if button.Animation == nil {
		t.Fatal("expected clock widget to provide animation")
	}
	if button.Animation.FrameRate != clockWidgetFrameRate {
		t.Fatalf("expected frame rate %d, got %d", clockWidgetFrameRate, button.Animation.FrameRate)
	}
	if !button.Animation.Loop {
		t.Fatal("expected clock widget animation to loop")
	}
}

func TestClockWidgetToggleChangesRenderedFrame(t *testing.T) {
	t.Parallel()

	widget, err := NewClockWidget(ClockWidgetOptions{
		Key: streamdeck.KEY_1,
		Now: func() time.Time { return time.Date(2026, time.March, 20, 10, 15, 30, 250_000_000, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewClockWidget: %v", err)
	}

	button := widget.Button()
	analog, err := button.Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt analog: %v", err)
	}

	widget.Toggle()
	if widget.Mode() != ClockModeDigital {
		t.Fatalf("expected mode digital after toggle, got %s", widget.Mode())
	}

	digital, err := button.Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt digital: %v", err)
	}

	if !reflect.DeepEqual(analog.Bounds(), image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
		t.Fatalf("unexpected analog bounds: %v", analog.Bounds())
	}
	if !reflect.DeepEqual(digital.Bounds(), image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
		t.Fatalf("unexpected digital bounds: %v", digital.Bounds())
	}
	if imagesEqual(analog, digital) {
		t.Fatal("expected analog and digital frames to differ")
	}
}

func TestClockWidgetFrameAtConcurrent(t *testing.T) {
	t.Parallel()

	widget, err := NewClockWidget(ClockWidgetOptions{
		Key: streamdeck.KEY_1,
		Now: func() time.Time { return time.Date(2026, time.March, 20, 10, 15, 30, 250_000_000, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewClockWidget: %v", err)
	}

	button := widget.Button()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				frame, err := button.Animation.Source.FrameAt(context.Background(), 0)
				if err != nil {
					t.Errorf("FrameAt: %v", err)
					return
				}
				if !frame.Bounds().Eq(image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
					t.Errorf("unexpected bounds: %v", frame.Bounds())
					return
				}
			}
		}()
	}
	wg.Wait()
}

func imagesEqual(a, b image.Image) bool {
	if !a.Bounds().Eq(b.Bounds()) {
		return false
	}

	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}

	return true
}
