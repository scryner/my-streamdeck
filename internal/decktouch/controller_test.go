package decktouch

import (
	"context"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

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
