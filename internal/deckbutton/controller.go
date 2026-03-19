package deckbutton

import (
	"context"
	"fmt"
	"image"
	"log"
	"sync"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

// FrameSource renders animation frames at a given timeline offset.
type FrameSource interface {
	Start(ctx context.Context) error
	FrameAt(ctx context.Context, elapsed time.Duration) (image.Image, error)
	Duration() time.Duration
	Close() error
}

// Animation defines how a key animation should be played.
type Animation struct {
	Source    FrameSource
	FrameRate int
	Duration  time.Duration
	Loop      bool
}

// Button represents a configurable Stream Deck key.
type Button struct {
	Key       streamdeck.KeyID
	Animation *Animation
	OnPress   streamdeck.KeyHandler
}

// Controller registers key handlers and runs key animations.
type Controller struct {
	device *streamdeck.Device

	ctx    context.Context
	cancel context.CancelFunc

	setImageMu sync.Mutex
	wg         sync.WaitGroup
}

func NewController(device *streamdeck.Device) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{
		device: device,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (c *Controller) RegisterButtons(buttons ...Button) error {
	for _, button := range buttons {
		if button.OnPress != nil {
			if err := c.device.AddKeyHandler(button.Key, button.OnPress); err != nil {
				return fmt.Errorf("register key handler for %s: %w", button.Key, err)
			}
		}
	}

	for _, button := range buttons {
		if button.Animation == nil {
			continue
		}

		if button.Animation.Source == nil {
			return fmt.Errorf("animation source is required for %s", button.Key)
		}
		if button.Animation.FrameRate <= 0 {
			return fmt.Errorf("animation frame rate must be > 0 for %s", button.Key)
		}

		if err := button.Animation.Source.Start(c.ctx); err != nil {
			return fmt.Errorf("start animation source for %s: %w", button.Key, err)
		}
		if err := c.renderFrame(button.Key, button.Animation.Source, 0); err != nil {
			_ = button.Animation.Source.Close()
			return fmt.Errorf("render initial frame for %s: %w", button.Key, err)
		}
		if !button.Animation.Loop && button.Animation.Duration <= 0 && button.Animation.Source.Duration() <= 0 {
			_ = button.Animation.Source.Close()
			continue
		}

		c.wg.Add(1)
		go func(button Button) {
			defer c.wg.Done()
			if err := c.runAnimation(button); err != nil {
				log.Printf("animation stopped for %s: %v", button.Key, err)
			}
		}(button)
	}

	return nil
}

func (c *Controller) Close() {
	c.cancel()
	c.wg.Wait()
}

func (c *Controller) runAnimation(button Button) error {
	anim := button.Animation
	defer anim.Source.Close()

	interval := time.Second / time.Duration(anim.FrameRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	duration := anim.Duration
	if duration <= 0 {
		duration = anim.Source.Duration()
	}

	startedAt := time.Now()
	for {
		select {
		case <-c.ctx.Done():
			return nil
		case <-ticker.C:
			elapsed := time.Since(startedAt)
			frameTime, isLast := normalizeFrameTime(elapsed, duration, anim.Loop)
			if err := c.renderFrame(button.Key, anim.Source, frameTime); err != nil {
				return fmt.Errorf("render frame for %s: %w", button.Key, err)
			}
			if isLast {
				return nil
			}
		}
	}
}

func (c *Controller) renderFrame(key streamdeck.KeyID, source FrameSource, elapsed time.Duration) error {
	img, err := source.FrameAt(c.ctx, elapsed)
	if err != nil {
		return err
	}

	c.setImageMu.Lock()
	defer c.setImageMu.Unlock()
	return c.device.SetKeyImage(key, img)
}

func normalizeFrameTime(elapsed time.Duration, duration time.Duration, loop bool) (time.Duration, bool) {
	if duration <= 0 {
		return elapsed, false
	}
	if loop {
		return elapsed % duration, false
	}
	if elapsed >= duration {
		return duration, true
	}
	return elapsed, false
}
