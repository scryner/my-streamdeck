package decktouch

import (
	"context"
	"image"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

type fakeSignedSource struct {
	sig        uint64
	frameCalls int
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

func TestRunDialRotateAggregatorBatchesWithinWindow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan dialRotateEvent, 4)
	got := make(chan int, 1)
	done := make(chan struct{})

	go func() {
		runDialRotateAggregator(ctx, 20*time.Millisecond, events, func(_ *streamdeck.Device, _ *streamdeck.Dial, steps int) error {
			got <- steps
			return nil
		})
		close(done)
	}()

	events <- dialRotateEvent{steps: 1}
	time.Sleep(5 * time.Millisecond)
	events <- dialRotateEvent{steps: 3}

	select {
	case steps := <-got:
		if steps != 4 {
			t.Fatalf("expected batched steps 4, got %d", steps)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for batched dial rotation")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("aggregator did not stop after cancellation")
	}
}

func TestRunDialRotateAggregatorFlushesSeparateBursts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan dialRotateEvent, 4)
	got := make(chan int, 2)
	done := make(chan struct{})

	go func() {
		runDialRotateAggregator(ctx, 15*time.Millisecond, events, func(_ *streamdeck.Device, _ *streamdeck.Dial, steps int) error {
			got <- steps
			return nil
		})
		close(done)
	}()

	events <- dialRotateEvent{steps: 2}
	select {
	case steps := <-got:
		if steps != 2 {
			t.Fatalf("expected first batch 2, got %d", steps)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for first batch")
	}

	events <- dialRotateEvent{steps: -1}
	select {
	case steps := <-got:
		if steps != -1 {
			t.Fatalf("expected second batch -1, got %d", steps)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for second batch")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("aggregator did not stop after cancellation")
	}
}

func TestRenderFrameSkipsWhenSourceSignatureUnchanged(t *testing.T) {
	t.Parallel()

	source := &fakeSignedSource{sig: 7}
	controller := &Controller{
		ctx:       context.Background(),
		lastFrame: map[WidgetID]renderRecord{WIDGET_1: {hasState: true, state: 7}},
	}

	if err := controller.renderFrame(WIDGET_1, source, 0); err != nil {
		t.Fatalf("renderFrame: %v", err)
	}
	if source.frameCalls != 0 {
		t.Fatalf("expected frame render to be skipped, got %d calls", source.frameCalls)
	}
}
