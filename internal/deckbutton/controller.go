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

// FrameSourceWithStateSignature can provide a cheap state hash before rendering.
// If the signature is unchanged, the controller can skip rendering and device I/O.
type FrameSourceWithStateSignature interface {
	StateSignature(ctx context.Context, elapsed time.Duration) (uint64, error)
}

// FrameSourceWithDynamicDelay can adjust its next render deadline based on current state.
type FrameSourceWithDynamicDelay interface {
	NextFrameDelay() time.Duration
}

// FrameSourceWithUpdates can wake the controller when state changes outside the regular cadence.
type FrameSourceWithUpdates interface {
	Updates() <-chan struct{}
}

// Animation defines how a key animation should be played.
type Animation struct {
	Source         FrameSource
	FrameRate      int
	UpdateInterval time.Duration
	Duration       time.Duration
	Loop           bool
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
	lastFrame  map[streamdeck.KeyID]renderRecord
	wg         sync.WaitGroup
}

type frameSignature struct {
	bounds image.Rectangle
	sum    uint64
}

type renderRecord struct {
	image    frameSignature
	state    uint64
	hasState bool
}

func NewController(device *streamdeck.Device) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{
		device:    device,
		ctx:       ctx,
		cancel:    cancel,
		lastFrame: map[streamdeck.KeyID]renderRecord{},
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
		if !animationHasSchedule(button.Animation) {
			return fmt.Errorf("animation frame rate or update interval is required for %s", button.Key)
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
	c.setImageMu.Lock()
	c.lastFrame = map[streamdeck.KeyID]renderRecord{}
	c.setImageMu.Unlock()
}

func (c *Controller) runAnimation(button Button) error {
	anim := button.Animation
	defer anim.Source.Close()

	if _, ok := anim.Source.(FrameSourceWithDynamicDelay); !ok {
		if _, ok := anim.Source.(FrameSourceWithUpdates); !ok {
			return c.runFixedAnimation(button)
		}
	}

	duration := anim.Duration
	if duration <= 0 {
		duration = anim.Source.Duration()
	}

	startedAt := time.Now()
	updates := updatesChannel(anim.Source)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	scheduleNext := func() {
		delay := nextFrameDelay(anim)
		if delay <= 0 {
			return
		}
		timer.Reset(delay)
	}

	scheduleNext()
	for {
		select {
		case <-c.ctx.Done():
			return nil
		case <-timer.C:
			elapsed := time.Since(startedAt)
			frameTime, isLast := normalizeFrameTime(elapsed, duration, anim.Loop)
			if err := c.renderFrame(button.Key, anim.Source, frameTime); err != nil {
				return fmt.Errorf("render frame for %s: %w", button.Key, err)
			}
			if isLast {
				return nil
			}
			scheduleNext()
		case <-updates:
			elapsed := time.Since(startedAt)
			frameTime, isLast := normalizeFrameTime(elapsed, duration, anim.Loop)
			if err := c.renderFrame(button.Key, anim.Source, frameTime); err != nil {
				return fmt.Errorf("render frame for %s: %w", button.Key, err)
			}
			if isLast {
				return nil
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			scheduleNext()
		}
	}
}

func (c *Controller) runFixedAnimation(button Button) error {
	anim := button.Animation
	interval := nextFrameDelay(anim)
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
	if stateSig, ok, err := sourceStateSignature(c.ctx, source, elapsed); err != nil {
		return err
	} else if ok {
		c.setImageMu.Lock()
		prev, found := c.lastFrame[key]
		c.setImageMu.Unlock()
		if found && prev.hasState && prev.state == stateSig {
			return nil
		}

		img, err := source.FrameAt(c.ctx, elapsed)
		if err != nil {
			return err
		}
		if postSig, postOK, postErr := sourceStateSignature(c.ctx, source, elapsed); postErr == nil && postOK {
			stateSig = postSig
		}

		c.setImageMu.Lock()
		defer c.setImageMu.Unlock()
		if err := c.device.SetKeyImage(key, img); err != nil {
			return err
		}
		c.lastFrame[key] = renderRecord{hasState: true, state: stateSig}
		return nil
	}

	img, err := source.FrameAt(c.ctx, elapsed)
	if err != nil {
		return err
	}

	c.setImageMu.Lock()
	defer c.setImageMu.Unlock()
	sig := imageSignature(img)
	if prev, ok := c.lastFrame[key]; ok && !prev.hasState && prev.image == sig {
		return nil
	}
	if err := c.device.SetKeyImage(key, img); err != nil {
		return err
	}
	c.lastFrame[key] = renderRecord{image: sig}
	return nil
}

func animationHasSchedule(anim *Animation) bool {
	if anim == nil || anim.Source == nil {
		return false
	}
	if anim.FrameRate > 0 || anim.UpdateInterval > 0 {
		return true
	}
	if _, ok := anim.Source.(FrameSourceWithDynamicDelay); ok {
		return true
	}
	if _, ok := anim.Source.(FrameSourceWithUpdates); ok {
		return true
	}
	return false
}

func sourceStateSignature(ctx context.Context, source FrameSource, elapsed time.Duration) (uint64, bool, error) {
	s, ok := source.(FrameSourceWithStateSignature)
	if !ok {
		return 0, false, nil
	}
	sig, err := s.StateSignature(ctx, elapsed)
	if err != nil {
		return 0, false, err
	}
	return sig, true, nil
}

func nextFrameDelay(anim *Animation) time.Duration {
	if source, ok := anim.Source.(FrameSourceWithDynamicDelay); ok {
		return source.NextFrameDelay()
	}
	if anim.UpdateInterval > 0 {
		return anim.UpdateInterval
	}
	if anim.FrameRate <= 0 {
		return 0
	}
	return time.Second / time.Duration(anim.FrameRate)
}

func updatesChannel(source FrameSource) <-chan struct{} {
	if sourceWithUpdates, ok := source.(FrameSourceWithUpdates); ok {
		return sourceWithUpdates.Updates()
	}
	return nil
}

func imagesEqual(a image.Image, b image.Image) bool {
	if a == nil || b == nil {
		return a == b
	}
	if !a.Bounds().Eq(b.Bounds()) {
		return false
	}

	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				return false
			}
		}
	}
	return true
}

func imageSignature(img image.Image) frameSignature {
	if img == nil {
		return frameSignature{}
	}

	sig := frameSignature{
		bounds: img.Bounds(),
		sum:    1469598103934665603,
	}

	switch src := img.(type) {
	case *image.RGBA:
		hashRGBABytes(&sig, src.Pix, src.Stride, sig.bounds.Dx(), sig.bounds.Dy())
		return sig
	case *image.NRGBA:
		hashRGBABytes(&sig, src.Pix, src.Stride, sig.bounds.Dx(), sig.bounds.Dy())
		return sig
	}

	for y := sig.bounds.Min.Y; y < sig.bounds.Max.Y; y++ {
		for x := sig.bounds.Min.X; x < sig.bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			sig.sum = fnv1a64(sig.sum, uint64(r))
			sig.sum = fnv1a64(sig.sum, uint64(g))
			sig.sum = fnv1a64(sig.sum, uint64(b))
			sig.sum = fnv1a64(sig.sum, uint64(a))
		}
	}

	return sig
}

func hashRGBABytes(sig *frameSignature, pix []uint8, stride int, width int, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	rowBytes := width * 4
	for y := 0; y < height; y++ {
		row := pix[y*stride : y*stride+rowBytes]
		for _, b := range row {
			sig.sum = fnv1a64(sig.sum, uint64(b))
		}
	}
}

func fnv1a64(sum uint64, value uint64) uint64 {
	sum ^= value
	return sum * 1099511628211
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
