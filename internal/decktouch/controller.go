package decktouch

import (
	"context"
	"fmt"
	"image"
	"log"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"rafaelmartins.com/p/streamdeck"
)

const dialRotateBatchWindow = 10 * time.Millisecond

// Controller registers touch-strip and dial handlers and renders touch widgets.
type Controller struct {
	device *streamdeck.Device
	bounds image.Rectangle

	ctx    context.Context
	cancel context.CancelFunc

	setImageMu sync.Mutex
	lastFrame  map[WidgetID]frameSignature
	widgets    map[WidgetID]*Widget
	wg         sync.WaitGroup
}

type frameSignature struct {
	bounds image.Rectangle
	sum    uint64
}

type dialRotateEvent struct {
	device *streamdeck.Device
	dial   *streamdeck.Dial
	steps  int
}

func NewController(device *streamdeck.Device) (*Controller, error) {
	bounds, err := device.GetTouchStripImageRectangle()
	if err != nil {
		return nil, fmt.Errorf("get touch strip bounds: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{
		device:    device,
		bounds:    bounds,
		ctx:       ctx,
		cancel:    cancel,
		lastFrame: map[WidgetID]frameSignature{},
		widgets:   map[WidgetID]*Widget{},
	}, nil
}

func (c *Controller) RegisterWidgets(widgets ...Widget) error {
	if len(widgets) == 0 {
		return nil
	}

	if err := c.device.ClearTouchStrip(); err != nil {
		return fmt.Errorf("clear touch strip: %w", err)
	}

	for _, widget := range widgets {
		widget := widget
		if err := widget.ID.Validate(); err != nil {
			return err
		}
		if _, exists := c.widgets[widget.ID]; exists {
			return fmt.Errorf("decktouch: duplicate widget id %s", widget.ID)
		}
		c.widgets[widget.ID] = &widget

		if widget.OnDialPress != nil {
			if err := c.device.AddDialSwitchHandler(widget.ID.DialID(), func(d *streamdeck.Device, dial *streamdeck.Dial) error {
				return widget.OnDialPress(d, &widget, dial)
			}); err != nil {
				return fmt.Errorf("register dial press handler for %s: %w", widget.ID, err)
			}
		}
		if widget.OnDialRotate != nil {
			events := c.startDialRotateAggregator(widget)
			if err := c.device.AddDialRotateHandler(widget.ID.DialID(), func(d *streamdeck.Device, dial *streamdeck.Dial, delta int8) error {
				select {
				case events <- dialRotateEvent{device: d, dial: dial, steps: int(delta)}:
				case <-c.ctx.Done():
				}
				return nil
			}); err != nil {
				return fmt.Errorf("register dial rotate handler for %s: %w", widget.ID, err)
			}
		}
	}

	if err := c.registerTouchHandlers(); err != nil {
		return err
	}

	for _, widget := range widgets {
		if widget.Animation == nil {
			continue
		}
		if widget.Animation.Source == nil {
			return fmt.Errorf("animation source is required for %s", widget.ID)
		}
		if widget.Animation.FrameRate <= 0 && widget.Animation.UpdateInterval <= 0 {
			return fmt.Errorf("animation frame rate or update interval is required for %s", widget.ID)
		}

		if err := widget.Animation.Source.Start(c.ctx); err != nil {
			return fmt.Errorf("start animation source for %s: %w", widget.ID, err)
		}
		if err := c.renderFrame(widget.ID, widget.Animation.Source, 0); err != nil {
			_ = widget.Animation.Source.Close()
			return fmt.Errorf("render initial frame for %s: %w", widget.ID, err)
		}
		if !widget.Animation.Loop && widget.Animation.Duration <= 0 && widget.Animation.Source.Duration() <= 0 {
			_ = widget.Animation.Source.Close()
			continue
		}

		c.wg.Add(1)
		go func(widget Widget) {
			defer c.wg.Done()
			if err := c.runAnimation(widget); err != nil {
				log.Printf("touch animation stopped for %s: %v", widget.ID, err)
			}
		}(widget)
	}

	return nil
}

func (c *Controller) startDialRotateAggregator(widget Widget) chan<- dialRotateEvent {
	events := make(chan dialRotateEvent, 64)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		runDialRotateAggregator(c.ctx, dialRotateBatchWindow, events, func(device *streamdeck.Device, dial *streamdeck.Dial, steps int) error {
			return widget.OnDialRotate(device, &widget, dial, steps)
		})
	}()
	return events
}

func runDialRotateAggregator(ctx context.Context, window time.Duration, events <-chan dialRotateEvent, handler func(device *streamdeck.Device, dial *streamdeck.Dial, steps int) error) {
	var (
		pending    int
		lastDevice *streamdeck.Device
		lastDial   *streamdeck.Dial
		timer      *time.Timer
		timerCh    <-chan time.Time
	)

	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerCh = nil
	}

	flush := func() {
		if pending == 0 || handler == nil {
			pending = 0
			return
		}
		steps := pending
		pending = 0
		if err := handler(lastDevice, lastDial, steps); err != nil {
			log.Printf("touch dial rotation handler failed: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			stopTimer()
			return
		case event := <-events:
			pending += event.steps
			lastDevice = event.device
			lastDial = event.dial

			if timer == nil {
				timer = time.NewTimer(window)
			} else {
				stopTimer()
				timer.Reset(window)
			}
			timerCh = timer.C
		case <-timerCh:
			stopTimer()
			flush()
		}
	}
}

func (c *Controller) Close() {
	c.cancel()
	c.wg.Wait()
	c.setImageMu.Lock()
	c.lastFrame = map[WidgetID]frameSignature{}
	c.setImageMu.Unlock()
}

func (c *Controller) registerTouchHandlers() error {
	if err := c.device.AddTouchStripTouchHandler(func(d *streamdeck.Device, typ streamdeck.TouchStripTouchType, p image.Point) error {
		id, ok := WidgetIDFromPoint(c.bounds, p)
		if !ok {
			return nil
		}
		widget := c.widgets[id]
		if widget == nil || widget.OnTouch == nil {
			return nil
		}
		local, ok := widget.LocalPoint(c.bounds, p)
		if !ok {
			return nil
		}
		return widget.OnTouch(d, widget, typ, local)
	}); err != nil {
		return fmt.Errorf("register touch strip touch handler: %w", err)
	}

	if err := c.device.AddTouchStripSwipeHandler(func(d *streamdeck.Device, origin image.Point, destination image.Point) error {
		originID, ok := WidgetIDFromPoint(c.bounds, origin)
		if !ok {
			return nil
		}
		destinationID, ok := WidgetIDFromPoint(c.bounds, destination)
		if !ok || destinationID != originID {
			return nil
		}
		widget := c.widgets[originID]
		if widget == nil || widget.OnSwipe == nil {
			return nil
		}
		localOrigin, ok := widget.LocalPoint(c.bounds, origin)
		if !ok {
			return nil
		}
		localDestination, ok := widget.LocalPoint(c.bounds, destination)
		if !ok {
			return nil
		}
		return widget.OnSwipe(d, widget, localOrigin, localDestination)
	}); err != nil {
		return fmt.Errorf("register touch strip swipe handler: %w", err)
	}

	return nil
}

func (c *Controller) runAnimation(widget Widget) error {
	anim := widget.Animation
	defer anim.Source.Close()

	if _, ok := anim.Source.(deckbutton.FrameSourceWithDynamicDelay); !ok {
		if _, ok := anim.Source.(deckbutton.FrameSourceWithUpdates); !ok {
			return c.runFixedAnimation(widget)
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
			if err := c.renderFrame(widget.ID, anim.Source, frameTime); err != nil {
				return fmt.Errorf("render frame for %s: %w", widget.ID, err)
			}
			if isLast {
				return nil
			}
			scheduleNext()
		case <-updates:
			elapsed := time.Since(startedAt)
			frameTime, isLast := normalizeFrameTime(elapsed, duration, anim.Loop)
			if err := c.renderFrame(widget.ID, anim.Source, frameTime); err != nil {
				return fmt.Errorf("render frame for %s: %w", widget.ID, err)
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

func (c *Controller) runFixedAnimation(widget Widget) error {
	anim := widget.Animation
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
			if err := c.renderFrame(widget.ID, anim.Source, frameTime); err != nil {
				return fmt.Errorf("render frame for %s: %w", widget.ID, err)
			}
			if isLast {
				return nil
			}
		}
	}
}

func (c *Controller) renderFrame(id WidgetID, source deckbutton.FrameSource, elapsed time.Duration) error {
	img, err := source.FrameAt(c.ctx, elapsed)
	if err != nil {
		return err
	}

	c.setImageMu.Lock()
	defer c.setImageMu.Unlock()
	sig := imageSignature(img)
	if prev, ok := c.lastFrame[id]; ok && prev == sig {
		return nil
	}
	if err := c.device.SetTouchStripImageWithRectangle(img, id.TouchStripRect(c.bounds)); err != nil {
		return err
	}
	c.lastFrame[id] = sig
	return nil
}

func nextFrameDelay(anim *Animation) time.Duration {
	if source, ok := anim.Source.(deckbutton.FrameSourceWithDynamicDelay); ok {
		return source.NextFrameDelay()
	}
	if anim.UpdateInterval > 0 {
		return anim.UpdateInterval
	}
	return time.Second / time.Duration(anim.FrameRate)
}

func updatesChannel(source deckbutton.FrameSource) <-chan struct{} {
	if sourceWithUpdates, ok := source.(deckbutton.FrameSourceWithUpdates); ok {
		return sourceWithUpdates.Updates()
	}
	return nil
}

func imageSignature(img image.Image) frameSignature {
	if img == nil {
		return frameSignature{}
	}

	sig := frameSignature{
		bounds: img.Bounds(),
		sum:    1469598103934665603,
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
