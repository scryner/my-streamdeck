package deckbutton

import (
	"context"
	"image"
	"image/color"
	"testing"
	"time"
)

type fakeDynamicSource struct {
	delay time.Duration
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
