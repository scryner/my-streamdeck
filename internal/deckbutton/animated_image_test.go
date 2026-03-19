package deckbutton

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAnimatedImageSourceGIF(t *testing.T) {
	t.Parallel()

	source, err := NewAnimatedImageSource(AnimatedImageSourceOptions{
		Path: filepath.Join("..", "..", "examples", "sample-animated.gif"),
	})
	if err != nil {
		t.Fatalf("NewAnimatedImageSource(gif): %v", err)
	}
	defer source.Close()

	if source.Duration() <= 0 {
		t.Fatalf("expected gif duration to be positive, got %s", source.Duration())
	}

	frame, err := source.FrameAt(context.Background(), 250*time.Millisecond)
	if err != nil {
		t.Fatalf("FrameAt(gif): %v", err)
	}
	if frame == nil {
		t.Fatal("expected gif frame image, got nil")
	}
}

func TestAnimatedImageSourceAPNG(t *testing.T) {
	t.Parallel()

	source, err := NewAnimatedImageSource(AnimatedImageSourceOptions{
		Path: filepath.Join("..", "..", "examples", "sample-animated.apng"),
	})
	if err != nil {
		t.Fatalf("NewAnimatedImageSource(apng): %v", err)
	}
	defer source.Close()

	if source.Duration() <= 0 {
		t.Fatalf("expected apng duration to be positive, got %s", source.Duration())
	}

	frame, err := source.FrameAt(context.Background(), 250*time.Millisecond)
	if err != nil {
		t.Fatalf("FrameAt(apng): %v", err)
	}
	if frame == nil {
		t.Fatal("expected apng frame image, got nil")
	}
}
