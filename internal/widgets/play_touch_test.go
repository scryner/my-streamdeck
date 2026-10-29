package widgets

import (
	"context"
	"image"
	"sync"
	"testing"
	"time"

	"github.com/scryner/my-streamdeck/internal/decktouch"
	"rafaelmartins.com/p/streamdeck"
)

type fakePlayerBackend struct {
	mu            sync.Mutex
	state         playerPlaybackState
	toggleCalls   int
	nextCalls     int
	previousCalls int
	// Test gates are configured before handing the backend to the widget.
	nextRelease <-chan struct{}
	nextEntered chan struct{}
	completed   chan string
	err         error
}

func newFakePlayerBackend(state playerPlaybackState) *fakePlayerBackend {
	return &fakePlayerBackend{state: state, completed: make(chan string, 16), nextEntered: make(chan struct{}, 16)}
}

func (f *fakePlayerBackend) State(context.Context) (playerPlaybackState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return playerPlaybackStateUnknown, f.err
	}
	return f.state, nil
}

func (f *fakePlayerBackend) Toggle(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toggleCalls++
	if f.state == playerPlaybackStatePlaying {
		f.state = playerPlaybackStatePaused
	} else {
		f.state = playerPlaybackStatePlaying
	}
	return f.err
}

func (f *fakePlayerBackend) Next(ctx context.Context) error {
	f.nextEntered <- struct{}{}
	if f.nextRelease != nil {
		select {
		case <-f.nextRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	f.nextCalls++
	err := f.err
	f.mu.Unlock()
	f.completed <- "next"
	return err
}

func (f *fakePlayerBackend) Previous(context.Context) error {
	f.mu.Lock()
	f.previousCalls++
	err := f.err
	f.mu.Unlock()
	f.completed <- "previous"
	return err
}

func (f *fakePlayerBackend) calls() (toggle, next, previous int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.toggleCalls, f.nextCalls, f.previousCalls
}

func waitForPlayerCall(t *testing.T, backend *fakePlayerBackend, want string) {
	t.Helper()
	select {
	case got := <-backend.completed:
		if got != want {
			t.Fatalf("player call = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", want)
	}
}

func assertNoPlayerCallDuringDebounce(t *testing.T, backend *fakePlayerBackend) {
	t.Helper()
	select {
	case got := <-backend.completed:
		t.Fatalf("unexpected duplicate player call: %s", got)
	case <-time.After(playSkipDebounceWindow):
	}
}

func TestPlayTouchWidgetMetadata(t *testing.T) {
	t.Parallel()

	widget, err := NewPlayTouchWidget(PlayTouchWidgetOptions{
		ID:     decktouch.WIDGET_4,
		Player: newFakePlayerBackend(playerPlaybackStateUnknown),
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

	backend := newFakePlayerBackend(playerPlaybackStatePaused)
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
	if calls, _, _ := backend.calls(); calls != 2 {
		t.Fatalf("expected 2 toggle calls, got %d", calls)
	}
}

func TestPlayTouchWidgetDialRotateNavigatesWithDebounce(t *testing.T) {
	t.Parallel()

	backend := newFakePlayerBackend(playerPlaybackStatePlaying)
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
	waitForPlayerCall(t, backend, "next")
	assertNoPlayerCallDuringDebounce(t, backend)
	if _, calls, _ := backend.calls(); calls != 1 {
		t.Fatalf("expected one next call, got %d", calls)
	}

	if err := touch.OnDialRotate(nil, &touch, nil, -2); err != nil {
		t.Fatalf("OnDialRotate previous: %v", err)
	}
	waitForPlayerCall(t, backend, "previous")
	if _, _, calls := backend.calls(); calls != 1 {
		t.Fatalf("expected one previous call, got %d", calls)
	}
}

func TestPlayTouchWidgetRenderProducesVisibleContent(t *testing.T) {
	t.Parallel()

	widget, err := NewPlayTouchWidget(PlayTouchWidgetOptions{
		ID:     decktouch.WIDGET_4,
		Player: newFakePlayerBackend(playerPlaybackStateUnknown),
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

	backend := newFakePlayerBackend(playerPlaybackStatePlaying)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	backend.nextRelease = release
	widget, err := NewPlayTouchWidget(PlayTouchWidgetOptions{
		ID:     decktouch.WIDGET_4,
		Player: backend,
	})
	if err != nil {
		t.Fatalf("NewPlayTouchWidget: %v", err)
	}

	touch := widget.Touch()
	returned := make(chan error, 1)
	go func() { returned <- touch.OnDialRotate(nil, &touch, nil, 1) }()
	select {
	case <-backend.nextEntered:
	case <-time.After(time.Second):
		t.Fatal("backend was never entered")
	}
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dial handler waited for the blocked backend")
	}
	if err := touch.OnDialRotate(nil, &touch, nil, 1); err != nil {
		t.Fatalf("OnDialRotate second debounced next: %v", err)
	}

	// Completion is controlled by the test, so elapsed wall time cannot be
	// mistaken for synchronization with the background command.
	releaseOnce.Do(func() { close(release) })
	waitForPlayerCall(t, backend, "next")
	assertNoPlayerCallDuringDebounce(t, backend)
	if _, calls, _ := backend.calls(); calls != 1 {
		t.Fatalf("expected one next call after debounce window, got %d", calls)
	}
}

func TestPlayTouchWidgetDialRotateShowsTransientSkipIcon(t *testing.T) {
	t.Parallel()

	backend := newFakePlayerBackend(playerPlaybackStatePlaying)
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

	waitForPlayerCall(t, backend, "next")
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for widget.source.currentVisualMode() != playerVisualModePlayPause {
		select {
		case <-widget.source.Updates():
		case <-deadline.C:
			t.Fatal("transient visual mode did not reset")
		}
	}
}
