package deckbutton

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kettek/apng"
)

const defaultFrameDelay = 100 * time.Millisecond

type AnimatedImageSourceOptions struct {
	Path string
}

type AnimatedImageSource struct {
	frames   []image.Image
	offsets  []time.Duration
	duration time.Duration
}

func NewAnimatedImageSource(options AnimatedImageSourceOptions) (*AnimatedImageSource, error) {
	if options.Path == "" {
		return nil, fmt.Errorf("animation image path is required")
	}

	file, err := os.Open(options.Path)
	if err != nil {
		return nil, fmt.Errorf("open animation image: %w", err)
	}
	defer file.Close()

	switch strings.ToLower(filepath.Ext(options.Path)) {
	case ".gif":
		return decodeGIF(file)
	case ".png", ".apng":
		return decodeAPNG(file)
	default:
		return nil, fmt.Errorf("unsupported animation image format: %s", filepath.Ext(options.Path))
	}
}

func (s *AnimatedImageSource) Start(context.Context) error {
	return nil
}

func (s *AnimatedImageSource) FrameAt(ctx context.Context, elapsed time.Duration) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if len(s.frames) == 0 {
		return nil, fmt.Errorf("animation has no frames")
	}
	if len(s.frames) == 1 || s.duration <= 0 {
		return s.frames[0], nil
	}

	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed >= s.duration {
		elapsed = s.duration - time.Nanosecond
	}

	for idx := len(s.offsets) - 1; idx >= 0; idx-- {
		if elapsed >= s.offsets[idx] {
			return s.frames[idx], nil
		}
	}

	return s.frames[0], nil
}

func (s *AnimatedImageSource) Duration() time.Duration {
	return s.duration
}

func (s *AnimatedImageSource) Close() error {
	return nil
}

func decodeGIF(file *os.File) (*AnimatedImageSource, error) {
	decoded, err := gif.DecodeAll(file)
	if err != nil {
		return nil, fmt.Errorf("decode gif: %w", err)
	}

	frames := make([]image.Image, 0, len(decoded.Image))
	offsets := make([]time.Duration, 0, len(decoded.Image))

	var total time.Duration
	for idx, src := range decoded.Image {
		frames = append(frames, cloneToRGBA(src))
		offsets = append(offsets, total)

		delay := defaultFrameDelay
		if idx < len(decoded.Delay) && decoded.Delay[idx] > 0 {
			delay = time.Duration(decoded.Delay[idx]) * 10 * time.Millisecond
		}
		total += delay
	}

	return &AnimatedImageSource{
		frames:   frames,
		offsets:  offsets,
		duration: total,
	}, nil
}

func decodeAPNG(file *os.File) (*AnimatedImageSource, error) {
	decoded, err := apng.DecodeAll(file)
	if err != nil {
		return nil, fmt.Errorf("decode apng: %w", err)
	}

	frames := make([]image.Image, 0, len(decoded.Frames))
	offsets := make([]time.Duration, 0, len(decoded.Frames))

	var total time.Duration
	for _, frame := range decoded.Frames {
		if frame.IsDefault {
			continue
		}

		frames = append(frames, cloneToRGBA(frame.Image))
		offsets = append(offsets, total)

		delay := time.Duration(frame.GetDelay() * float64(time.Second))
		if delay <= 0 {
			delay = defaultFrameDelay
		}
		total += delay
	}

	if len(frames) == 0 && len(decoded.Frames) > 0 {
		frames = append(frames, cloneToRGBA(decoded.Frames[0].Image))
		offsets = append(offsets, 0)
		total = 0
	}

	return &AnimatedImageSource{
		frames:   frames,
		offsets:  offsets,
		duration: total,
	}, nil
}

func cloneToRGBA(src image.Image) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	return dst
}
