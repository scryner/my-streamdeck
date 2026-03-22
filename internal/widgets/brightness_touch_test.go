package widgets

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/scryner/my-streamdeck/internal/decktouch"
)

type fakeBrightnessBackend struct {
	state    BrightnessState
	err      error
	setCalls []int
}

func (f *fakeBrightnessBackend) State(context.Context) (BrightnessState, error) {
	if f.err != nil {
		return BrightnessState{}, f.err
	}
	return f.state, nil
}

func (f *fakeBrightnessBackend) SetBrightness(_ context.Context, percent int) error {
	f.state.Brightness = percent
	f.setCalls = append(f.setCalls, percent)
	return nil
}

func TestBrightnessTouchWidgetMetadata(t *testing.T) {
	t.Parallel()

	backend := &fakeBrightnessBackend{state: BrightnessState{Display: "Studio Display", Brightness: 50}}
	widget, err := NewBrightnessTouchWidget(BrightnessTouchWidgetOptions{
		ID:         decktouch.WIDGET_3,
		Brightness: backend,
	})
	if err != nil {
		t.Fatalf("NewBrightnessTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if touch.ID != decktouch.WIDGET_3 {
		t.Fatalf("unexpected widget id: %s", touch.ID)
	}
	if touch.OnDialRotate == nil {
		t.Fatal("expected dial rotate handler to be registered")
	}
	if touch.OnTouch != nil {
		t.Fatal("expected touch handler to be nil")
	}
	if touch.OnDialPress != nil {
		t.Fatal("expected dial press handler to be nil")
	}
	if touch.Animation == nil {
		t.Fatal("expected animation to be configured")
	}
	if touch.Animation.UpdateInterval != brightnessTouchUpdateInterval {
		t.Fatalf("unexpected update interval: got %s want %s", touch.Animation.UpdateInterval, brightnessTouchUpdateInterval)
	}
}

func TestBrightnessTouchWidgetDialRotateAdjustsBrightnessByFour(t *testing.T) {
	t.Parallel()

	backend := &fakeBrightnessBackend{state: BrightnessState{Display: "Studio Display", Brightness: 50}}
	widget, err := NewBrightnessTouchWidget(BrightnessTouchWidgetOptions{
		ID:         decktouch.WIDGET_3,
		Brightness: backend,
	})
	if err != nil {
		t.Fatalf("NewBrightnessTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnDialRotate(nil, &touch, nil, 2); err != nil {
		t.Fatalf("OnDialRotate: %v", err)
	}
	if backend.state.Brightness != 58 {
		t.Fatalf("expected brightness 58, got %d", backend.state.Brightness)
	}

	if err := touch.OnDialRotate(nil, &touch, nil, -20); err != nil {
		t.Fatalf("OnDialRotate clamp: %v", err)
	}
	if backend.state.Brightness != 0 {
		t.Fatalf("expected brightness 0, got %d", backend.state.Brightness)
	}
}

func TestBrightnessTouchWidgetRenderProducesVisibleContent(t *testing.T) {
	t.Parallel()

	backend := &fakeBrightnessBackend{state: BrightnessState{
		Display:    "Studio Display",
		Brightness: 42,
	}}
	widget, err := NewBrightnessTouchWidget(BrightnessTouchWidgetOptions{
		ID:         decktouch.WIDGET_3,
		Brightness: backend,
	})
	if err != nil {
		t.Fatalf("NewBrightnessTouchWidget: %v", err)
	}

	frame, err := widget.Touch().Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}
	if !frame.Bounds().Eq(image.Rect(0, 0, defaultTouchWidgetWidth, defaultTouchWidgetHeight)) {
		t.Fatalf("unexpected bounds: %v", frame.Bounds())
	}

	visiblePixels := 0
	for y := 0; y < frame.Bounds().Dy(); y++ {
		for x := 0; x < frame.Bounds().Dx(); x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if maxUint32(r, g, b) > 0x7000 {
				visiblePixels++
			}
		}
	}
	if visiblePixels == 0 {
		t.Fatal("expected brightness touch widget to render visible content")
	}
}

func TestBrightnessTouchWidgetRenderNoControlOnBackendError(t *testing.T) {
	t.Parallel()

	backend := &fakeBrightnessBackend{err: errors.New("unsupported display brightness")}
	widget, err := NewBrightnessTouchWidget(BrightnessTouchWidgetOptions{
		ID:         decktouch.WIDGET_3,
		Brightness: backend,
	})
	if err != nil {
		t.Fatalf("NewBrightnessTouchWidget: %v", err)
	}

	frame, err := widget.Touch().Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}

	visiblePixels := 0
	for y := 0; y < frame.Bounds().Dy(); y++ {
		for x := 0; x < frame.Bounds().Dx(); x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if maxUint32(r, g, b) > 0x7000 {
				visiblePixels++
			}
		}
	}
	if visiblePixels == 0 {
		t.Fatal("expected placeholder content to render when brightness control is unavailable")
	}
}
