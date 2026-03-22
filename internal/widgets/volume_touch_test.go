package widgets

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/scryner/my-streamdeck/internal/decktouch"
	"rafaelmartins.com/p/streamdeck"
)

type fakeVolumeBackend struct {
	state       VolumeState
	setCalls    []int
	toggleCalls int
}

func (f *fakeVolumeBackend) State(context.Context) (VolumeState, error) {
	return f.state, nil
}

func (f *fakeVolumeBackend) SetVolume(_ context.Context, percent int) error {
	f.state.Volume = percent
	f.setCalls = append(f.setCalls, percent)
	return nil
}

func (f *fakeVolumeBackend) ToggleMute(context.Context) error {
	f.toggleCalls++
	f.state.Muted = !f.state.Muted
	return nil
}

func TestVolumeTouchWidgetTouchMetadata(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 25}}
	widget, err := NewVolumeTouchWidget(VolumeTouchWidgetOptions{
		ID:    decktouch.WIDGET_1,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewVolumeTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if touch.ID != decktouch.WIDGET_1 {
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
	if touch.Animation.UpdateInterval != volumeTouchUpdateInterval {
		t.Fatalf("unexpected update interval: got %s want %s", touch.Animation.UpdateInterval, volumeTouchUpdateInterval)
	}
}

func TestVolumeTouchWidgetTogglesMuteOnShortTouch(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 25}}
	widget, err := NewVolumeTouchWidget(VolumeTouchWidgetOptions{
		ID:    decktouch.WIDGET_1,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewVolumeTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnTouch(nil, &touch, streamdeck.TOUCH_STRIP_TOUCH_TYPE_SHORT, image.Point{}); err != nil {
		t.Fatalf("OnTouch: %v", err)
	}

	if !backend.state.Muted {
		t.Fatal("expected mute state to toggle on short touch")
	}
	if backend.toggleCalls != 1 {
		t.Fatalf("expected one toggle call, got %d", backend.toggleCalls)
	}
}

func TestVolumeTouchWidgetDialRotateAdjustsVolumeByFour(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 25}}
	widget, err := NewVolumeTouchWidget(VolumeTouchWidgetOptions{
		ID:    decktouch.WIDGET_2,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewVolumeTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnDialRotate(nil, &touch, nil, 1); err != nil {
		t.Fatalf("OnDialRotate: %v", err)
	}
	if backend.state.Volume != 29 {
		t.Fatalf("expected volume 29, got %d", backend.state.Volume)
	}

	if err := touch.OnDialRotate(nil, &touch, nil, -2); err != nil {
		t.Fatalf("OnDialRotate second step: %v", err)
	}
	if backend.state.Volume != 21 {
		t.Fatalf("expected volume 21, got %d", backend.state.Volume)
	}
}

func TestVolumeTouchWidgetDialPressTogglesMute(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 25}}
	widget, err := NewVolumeTouchWidget(VolumeTouchWidgetOptions{
		ID:    decktouch.WIDGET_2,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewVolumeTouchWidget: %v", err)
	}

	touch := widget.Touch()
	if err := touch.OnDialPress(nil, &touch, nil); err != nil {
		t.Fatalf("OnDialPress: %v", err)
	}

	if !backend.state.Muted {
		t.Fatal("expected mute state to toggle on dial press")
	}
	if backend.toggleCalls != 1 {
		t.Fatalf("expected one toggle call, got %d", backend.toggleCalls)
	}
}

func TestVolumeTouchWidgetRenderProducesVisibleContent(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{
		Source: "Studio Display XDR",
		Volume: 25,
	}}
	widget, err := NewVolumeTouchWidget(VolumeTouchWidgetOptions{
		ID:    decktouch.WIDGET_1,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewVolumeTouchWidget: %v", err)
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
		t.Fatal("expected volume touch widget to render visible content")
	}
}

func TestVolumeSystemBackendNormalizesSourceName(t *testing.T) {
	t.Parallel()

	backend := &volumeSystemBackend{
		runCommand: func(_ context.Context, name string, _ ...string) ([]byte, error) {
			switch name {
			case "/usr/bin/osascript":
				return []byte("28,false"), nil
			case "/usr/sbin/system_profiler":
				return []byte(`{"SPAudioDataType":[{"_items":[{"_name":"Studio Display XDR 스피커","coreaudio_default_audio_output_device":"spaudio_yes"}]}]}`), nil
			default:
				t.Fatalf("unexpected command: %s", name)
				return nil, nil
			}
		},
	}

	state, err := backend.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.Source != "Studio Display XDR" {
		t.Fatalf("unexpected source: %q", state.Source)
	}
}

func TestVolumeSystemBackendSetVolumeDoesNotMuteWhenUnmuted(t *testing.T) {
	t.Parallel()

	var scripts []string
	backend := &volumeSystemBackend{
		runCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "/usr/bin/osascript" {
				t.Fatalf("unexpected command: %s", name)
			}
			script := strings.Join(args, " ")
			scripts = append(scripts, script)
			if strings.Contains(script, "get volume settings") {
				return []byte("4,false"), nil
			}
			return nil, nil
		},
	}

	if err := backend.SetVolume(context.Background(), 0); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}

	last := scripts[len(scripts)-1]
	if !strings.Contains(last, "set volume output volume 0 without output muted") {
		t.Fatalf("expected unmuted volume set script, got %q", last)
	}
}

func TestVolumeSystemBackendSetVolumePreservesMutedState(t *testing.T) {
	t.Parallel()

	var scripts []string
	backend := &volumeSystemBackend{
		runCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "/usr/bin/osascript" {
				t.Fatalf("unexpected command: %s", name)
			}
			script := strings.Join(args, " ")
			scripts = append(scripts, script)
			if strings.Contains(script, "get volume settings") {
				return []byte("24,true"), nil
			}
			return nil, nil
		},
	}

	if err := backend.SetVolume(context.Background(), 16); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}

	last := scripts[len(scripts)-1]
	if !strings.Contains(last, "set volume output volume 16 with output muted") {
		t.Fatalf("expected muted volume set script, got %q", last)
	}
}

func TestVolumeSourceNotifiesOnStateChange(t *testing.T) {
	t.Parallel()

	backend := &fakeVolumeBackend{state: VolumeState{Source: "Studio Display XDR", Volume: 25}}
	widget, err := NewVolumeTouchWidget(VolumeTouchWidgetOptions{
		ID:    decktouch.WIDGET_1,
		Audio: backend,
	})
	if err != nil {
		t.Fatalf("NewVolumeTouchWidget: %v", err)
	}

	select {
	case <-widget.source.Updates():
		t.Fatal("unexpected update before notification")
	default:
	}

	widget.source.notify()

	select {
	case <-widget.source.Updates():
	case <-time.After(time.Second):
		t.Fatal("expected update notification")
	}
}

func TestDrawVolumeSliderFollowsProgress(t *testing.T) {
	t.Parallel()

	low := image.NewRGBA(image.Rect(0, 0, 120, 24))
	high := image.NewRGBA(image.Rect(0, 0, 120, 24))

	drawVolumeSlider(low, 10, 7, 100, 10, 0.25)
	drawVolumeSlider(high, 10, 7, 100, 10, 0.75)

	lowFilled := countSliderPixels(low, color.RGBA{R: 248, G: 248, B: 249, A: 255})
	highFilled := countSliderPixels(high, color.RGBA{R: 248, G: 248, B: 249, A: 255})
	if highFilled <= lowFilled {
		t.Fatalf("expected 75%% fill (%d) to exceed 25%% fill (%d)", highFilled, lowFilled)
	}
}

func countSliderPixels(img *image.RGBA, target color.RGBA) int {
	count := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if img.RGBAAt(x, y) == target {
				count++
			}
		}
	}
	return count
}
