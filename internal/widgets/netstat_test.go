package widgets

import (
	"context"
	"image"
	"reflect"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

func TestNetstatWidgetRendersExpectedBounds(t *testing.T) {
	t.Parallel()

	calls := 0
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	widget, err := NewNetstatWidget(NetstatWidgetOptions{
		Key:       streamdeck.KEY_4,
		Interface: "en0",
		Now: func() time.Time {
			return now
		},
		Stats: func(context.Context, string) (uint64, uint64, error) {
			calls++
			if calls == 1 {
				return 1024, 2048, nil
			}
			return 4096, 6144, nil
		},
	})
	if err != nil {
		t.Fatalf("NewNetstatWidget: %v", err)
	}

	button := widget.Button()
	if button.Animation == nil {
		t.Fatal("expected netstat widget to provide animation")
	}
	if button.Animation.FrameRate != netstatWidgetFrameRate {
		t.Fatalf("expected frame rate %d, got %d", netstatWidgetFrameRate, button.Animation.FrameRate)
	}

	frame, err := button.Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt first sample: %v", err)
	}
	if !reflect.DeepEqual(frame.Bounds(), image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
		t.Fatalf("unexpected netstat bounds: %v", frame.Bounds())
	}

	now = now.Add(time.Second)
	frame, err = button.Animation.Source.FrameAt(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("FrameAt second sample: %v", err)
	}
	if !reflect.DeepEqual(frame.Bounds(), image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
		t.Fatalf("unexpected netstat bounds after update: %v", frame.Bounds())
	}
	if calls != 2 {
		t.Fatalf("expected stats provider to be called twice, got %d", calls)
	}

	topPixels := 0
	middlePixels := 0
	bottomPixels := 0
	for y := 0; y < DefaultClockWidgetSize; y++ {
		for x := 0; x < DefaultClockWidgetSize; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if maxUint32(r, g, b) <= 0x7000 {
				continue
			}

			switch {
			case y < DefaultClockWidgetSize/3:
				topPixels++
			case y < (DefaultClockWidgetSize*4)/5:
				middlePixels++
			default:
				bottomPixels++
			}
		}
	}

	if topPixels == 0 {
		t.Fatal("expected incoming bandwidth rendering in the first row")
	}
	if middlePixels == 0 {
		t.Fatal("expected outgoing bandwidth rendering in the second row")
	}
	if bottomPixels == 0 {
		t.Fatal("expected interface label rendering in the third row")
	}
}

func TestNetstatSourceReadRates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	source := &netstatSource{
		iface: "en0",
		now: func() time.Time {
			return now
		},
		stats: func(context.Context, string) (uint64, uint64, error) {
			if now.Equal(time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)) {
				return 1000, 2000, nil
			}
			return 2500, 5000, nil
		},
	}

	inRate, outRate, err := source.readRates(context.Background())
	if err != nil {
		t.Fatalf("readRates first sample: %v", err)
	}
	if inRate != 0 || outRate != 0 {
		t.Fatalf("expected zero rates on first sample, got in=%f out=%f", inRate, outRate)
	}

	now = now.Add(2 * time.Second)
	inRate, outRate, err = source.readRates(context.Background())
	if err != nil {
		t.Fatalf("readRates second sample: %v", err)
	}
	if inRate != 750 {
		t.Fatalf("expected incoming rate 750 B/s, got %f", inRate)
	}
	if outRate != 1500 {
		t.Fatalf("expected outgoing rate 1500 B/s, got %f", outRate)
	}
}

func TestFormatBandwidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input float64
		want  string
	}{
		{input: 0, want: "0B/s"},
		{input: 999, want: "999B/s"},
		{input: 1500, want: "1.5K/s"},
		{input: 12_500_000, want: "12.5M/s"},
	}

	for _, tc := range tests {
		if got := formatBandwidth(tc.input); got != tc.want {
			t.Fatalf("formatBandwidth(%f) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
