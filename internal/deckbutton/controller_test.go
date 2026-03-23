package deckbutton

import (
	"context"
	"image"
	"image/color"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

type fakeDynamicSource struct {
	delay time.Duration
}

type fakeSignedSource struct {
	sig        uint64
	frameCalls int
}

func (f fakeDynamicSource) Start(context.Context) error {
	return nil
}

func (f fakeDynamicSource) FrameAt(context.Context, time.Duration) (image.Image, error) {
	return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
}

func (f fakeDynamicSource) Duration() time.Duration {
	return 0
}

func (f fakeDynamicSource) Close() error {
	return nil
}

func (f fakeDynamicSource) NextFrameDelay() time.Duration {
	return f.delay
}

func (f *fakeSignedSource) Start(context.Context) error {
	return nil
}

func (f *fakeSignedSource) FrameAt(context.Context, time.Duration) (image.Image, error) {
	f.frameCalls++
	return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
}

func (f *fakeSignedSource) StateSignature(context.Context, time.Duration) (uint64, error) {
	return f.sig, nil
}

func (f *fakeSignedSource) Duration() time.Duration {
	return 0
}

func (f *fakeSignedSource) Close() error {
	return nil
}

func TestImagesEqual(t *testing.T) {
	t.Parallel()

	a := image.NewRGBA(image.Rect(0, 0, 2, 2))
	b := image.NewRGBA(image.Rect(0, 0, 2, 2))
	a.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	b.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})

	if !imagesEqual(a, b) {
		t.Fatal("expected identical images to compare equal")
	}

	b.Set(1, 1, color.RGBA{R: 99, G: 88, B: 77, A: 255})
	if imagesEqual(a, b) {
		t.Fatal("expected differing images to compare unequal")
	}
}

func TestNextFrameDelayUsesDynamicSource(t *testing.T) {
	t.Parallel()

	anim := &Animation{
		Source:         fakeDynamicSource{delay: 250 * time.Millisecond},
		FrameRate:      30,
		UpdateInterval: time.Second,
	}

	if got := nextFrameDelay(anim); got != 250*time.Millisecond {
		t.Fatalf("expected dynamic delay to win, got %s", got)
	}
}

func TestImageSignatureChangesWithPixels(t *testing.T) {
	t.Parallel()

	a := image.NewRGBA(image.Rect(0, 0, 2, 2))
	b := image.NewRGBA(image.Rect(0, 0, 2, 2))

	sigA := imageSignature(a)
	sigB := imageSignature(b)
	if sigA != sigB {
		t.Fatal("expected identical images to have identical signatures")
	}

	b.Set(1, 1, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	sigB = imageSignature(b)
	if sigA == sigB {
		t.Fatal("expected differing images to have differing signatures")
	}
}

func TestRenderFrameSkipsWhenSourceSignatureUnchanged(t *testing.T) {
	t.Parallel()

	source := &fakeSignedSource{sig: 42}
	controller := &Controller{
		ctx:       context.Background(),
		lastFrame: map[streamdeck.KeyID]renderRecord{streamdeck.KEY_1: {hasState: true, state: 42}},
	}

	if err := controller.renderFrame(streamdeck.KEY_1, source, 0); err != nil {
		t.Fatalf("renderFrame: %v", err)
	}
	if source.frameCalls != 0 {
		t.Fatalf("expected frame render to be skipped, got %d calls", source.frameCalls)
	}
}

func TestAnimationHasScheduleAllowsUpdateOnlySources(t *testing.T) {
	t.Parallel()

	updateAnim := &Animation{
		Source: updateOnlySource{fakeSignedSource: &fakeSignedSource{sig: 1}, updates: make(chan struct{})},
	}

	if !animationHasSchedule(updateAnim) {
		t.Fatal("expected update-only source to be schedulable")
	}
	if animationHasSchedule(&Animation{Source: &fakeSignedSource{sig: 1}}) {
		t.Fatal("expected source without cadence or updates to require schedule")
	}
}

type updateOnlySource struct {
	*fakeSignedSource
	updates chan struct{}
}

func (s updateOnlySource) Updates() <-chan struct{} {
	return s.updates
}
