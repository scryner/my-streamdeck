package widgets

import (
	"context"
	"image"
	"testing"
	"time"

	"github.com/scryner/my-streamdeck/internal/decktouch"
	"rafaelmartins.com/p/streamdeck"
)

type fakePlayerBackend struct {
	state         playerPlaybackState
	toggleCalls   int
	nextCalls     int
	previousCalls int
	nextDelay     time.Duration
	prevDelay     time.Duration
	err           error
}

func (f *fakePlayerBackend) State(context.Context) (playerPlaybackState, error) {
	if f.err != nil {
		return playerPlaybackStateUnknown, f.err
	}
	return f.state, nil
}

func (f *fakePlayerBackend) Toggle(context.Context) error {
	f.toggleCalls++
	if f.state == playerPlaybackStatePlaying {
		f.state = playerPlaybackStatePaused
	} else {
		f.state = playerPlaybackStatePlaying
	}
	return f.err
}

func (f *fakePlayerBackend) Next(context.Context) error {
	if f.nextDelay > 0 {
		time.Sleep(f.nextDelay)
	}
	f.nextCalls++
	return f.err
}

func (f *fakePlayerBackend) Previous(context.Context) error {
	if f.prevDelay > 0 {
		time.Sleep(f.prevDelay)
	}
	f.previousCalls++
	return f.err
}

func TestPlayTouchWidgetMetadata(t *testing.T) {
	t.Parallel()

	widget, err := NewPlayTouchWidget(PlayTouchWidgetOptions{
		ID:     decktouch.WIDGET_4,
		Player: &fakePlayerBackend{state: playerPlaybackStateUnknown},
	})
	if err != nil {
		t.Fatalf("NewPlayTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if touch.ID != decktouch.WIDGET_4 {
		t.Fatalf("unexpected widget id: %s", touch.ID)
	}
	if touch.OnTouch == nil || touch.OnDialPress == nil || touch.OnDialRotate == nil {
		t.Fatal("expected play touch widget handlers to be registered")
	}
	if touch.Animation == nil {
		t.Fatal("expected animation to be configured")
	}
}

func TestPlayTouchWidgetTouchAndDialPressTogglePlayback(t *testing.T) {
	t.Parallel()

	backend := &fakePlayerBackend{state: playerPlaybackStatePaused}
	widget, err := NewPlayTouchWidget(PlayTouchWidgetOptions{
		ID:     decktouch.WIDGET_4,
		Player: backend,
	})
	if err != nil {
		t.Fatalf("NewPlayTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnTouch(nil, &touch, streamdeck.TOUCH_STRIP_TOUCH_TYPE_SHORT, image.Point{}); err != nil {
		t.Fatalf("OnTouch: %v", err)
	}
	if err := touch.OnDialPress(nil, &touch, nil); err != nil {
		t.Fatalf("OnDialPress: %v", err)
	}
	if backend.toggleCalls != 2 {
		t.Fatalf("expected 2 toggle calls, got %d", backend.toggleCalls)
	}
}

func TestPlayTouchWidgetDialRotateNavigatesWithDebounce(t *testing.T) {
	t.Parallel()

	backend := &fakePlayerBackend{state: playerPlaybackStatePlaying}
	widget, err := NewPlayTouchWidget(PlayTouchWidgetOptions{
		ID:     decktouch.WIDGET_4,
		Player: backend,
	})
	if err != nil {
		t.Fatalf("NewPlayTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnDialRotate(nil, &touch, nil, 3); err != nil {
		t.Fatalf("OnDialRotate first next: %v", err)
	}
	if err := touch.OnDialRotate(nil, &touch, nil, 1); err != nil {
		t.Fatalf("OnDialRotate debounced next: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if backend.nextCalls != 1 {
		t.Fatalf("expected one next call, got %d", backend.nextCalls)
	}

	time.Sleep(playSkipDebounceWindow + 10*time.Millisecond)

	if err := touch.OnDialRotate(nil, &touch, nil, -2); err != nil {
		t.Fatalf("OnDialRotate previous: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if backend.previousCalls != 1 {
		t.Fatalf("expected one previous call, got %d", backend.previousCalls)
	}
}

func TestPlayTouchWidgetRenderProducesVisibleContent(t *testing.T) {
	t.Parallel()

	widget, err := NewPlayTouchWidget(PlayTouchWidgetOptions{
		ID:     decktouch.WIDGET_4,
		Player: &fakePlayerBackend{state: playerPlaybackStateUnknown},
	})
	if err != nil {
		t.Fatalf("NewPlayTouchWidget: %v", err)
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
		t.Fatal("expected play touch widget to render visible content")
	}
}

func TestPlayTouchWidgetDialRotateDebouncesSlowBackendByInputTime(t *testing.T) {
	t.Parallel()

	backend := &fakePlayerBackend{
		state:     playerPlaybackStatePlaying,
		nextDelay: 200 * time.Millisecond,
	}
	widget, err := NewPlayTouchWidget(PlayTouchWidgetOptions{
		ID:     decktouch.WIDGET_4,
		Player: backend,
	})
	if err != nil {
		t.Fatalf("NewPlayTouchWidget: %v", err)
	}

	touch := widget.Touch()
	start := time.Now()
	if err := touch.OnDialRotate(nil, &touch, nil, 1); err != nil {
		t.Fatalf("OnDialRotate first next: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("expected first rotate to return quickly, got %s", elapsed)
	}

	time.Sleep(20 * time.Millisecond)

	if err := touch.OnDialRotate(nil, &touch, nil, 1); err != nil {
		t.Fatalf("OnDialRotate second debounced next: %v", err)
	}

	time.Sleep(backend.nextDelay + 80*time.Millisecond)

	if backend.nextCalls != 1 {
		t.Fatalf("expected one next call after debounce window, got %d", backend.nextCalls)
	}
}

func TestPlayTouchWidgetDialRotateShowsTransientSkipIcon(t *testing.T) {
	t.Parallel()

	backend := &fakePlayerBackend{state: playerPlaybackStatePlaying}
	widget, err := NewPlayTouchWidget(PlayTouchWidgetOptions{
		ID:     decktouch.WIDGET_4,
		Player: backend,
	})
	if err != nil {
		t.Fatalf("NewPlayTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnDialRotate(nil, &touch, nil, 1); err != nil {
		t.Fatalf("OnDialRotate next: %v", err)
	}
	if mode := widget.source.currentVisualMode(); mode != playerVisualModeNext {
		t.Fatalf("expected transient next icon, got %v", mode)
	}

	time.Sleep(playSkipDebounceWindow + 40*time.Millisecond)

	if mode := widget.source.currentVisualMode(); mode != playerVisualModePlayPause {
		t.Fatalf("expected visual mode to reset, got %v", mode)
	}
}
