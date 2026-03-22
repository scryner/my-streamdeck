package widgets

import (
	"context"
	"image"
	"testing"

	"github.com/scryner/my-streamdeck/internal/decktouch"
	"rafaelmartins.com/p/streamdeck"
)

func TestMicrophoneTouchWidgetTouchMetadata(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 50}}
	widget, err := NewMicrophoneTouchWidget(MicrophoneTouchWidgetOptions{
		ID:    decktouch.WIDGET_2,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewMicrophoneTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if touch.ID != decktouch.WIDGET_2 {
		t.Fatalf("unexpected widget id: %s", touch.ID)
	}
	if touch.OnTouch == nil {
		t.Fatal("expected touch handler to be registered")
	}
	if touch.OnDialPress == nil {
		t.Fatal("expected dial press handler to be registered")
	}
	if touch.OnDialRotate == nil {
		t.Fatal("expected dial rotate handler to be registered")
	}
	if touch.Animation == nil {
		t.Fatal("expected animation to be configured")
	}
}

func TestMicrophoneTouchWidgetTogglesMuteOnShortTouch(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 50}}
	widget, err := NewMicrophoneTouchWidget(MicrophoneTouchWidgetOptions{
		ID:    decktouch.WIDGET_2,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewMicrophoneTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnTouch(nil, &touch, streamdeck.TOUCH_STRIP_TOUCH_TYPE_SHORT, image.Point{}); err != nil {
		t.Fatalf("OnTouch: %v", err)
	}
	if !backend.state.Muted {
		t.Fatal("expected mute state to toggle on short touch")
	}
}

func TestMicrophoneTouchWidgetDialRotateAdjustsVolumeByFour(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 50}}
	widget, err := NewMicrophoneTouchWidget(MicrophoneTouchWidgetOptions{
		ID:    decktouch.WIDGET_2,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewMicrophoneTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnDialRotate(nil, &touch, nil, -2); err != nil {
		t.Fatalf("OnDialRotate: %v", err)
	}
	if backend.state.Volume != 42 {
		t.Fatalf("expected volume 42, got %d", backend.state.Volume)
	}
}

func TestMicrophoneTouchWidgetDialPressTogglesMute(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 50}}
	widget, err := NewMicrophoneTouchWidget(MicrophoneTouchWidgetOptions{
		ID:    decktouch.WIDGET_2,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewMicrophoneTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnDialPress(nil, &touch, nil); err != nil {
		t.Fatalf("OnDialPress: %v", err)
	}
	if !backend.state.Muted {
		t.Fatal("expected mute state to toggle on dial press")
	}
}

func TestMicrophoneTouchWidgetRenderProducesVisibleContent(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 50}}
	widget, err := NewMicrophoneTouchWidget(MicrophoneTouchWidgetOptions{
		ID:    decktouch.WIDGET_2,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewMicrophoneTouchWidget: %v", err)
	}

	frame, err := widget.Touch().Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}
	if !frame.Bounds().Eq(image.Rect(0, 0, defaultTouchWidgetWidth, defaultTouchWidgetHeight)) {
		t.Fatalf("unexpected bounds: %v", frame.Bounds())
	}
}
