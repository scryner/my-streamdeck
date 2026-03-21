package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"os/exec"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"rafaelmartins.com/p/streamdeck"
)

const (
	caffeinateWidgetFrameRate = 30
	caffeinateGaugeDuration   = 17 * time.Second / 12
	caffeinateTapDuration     = 250 * time.Millisecond
	caffeinateGaugeEaseExp    = 2.8
)

type CaffeinateBackend interface {
	Enable() error
	Disable() error
	Sleep() error
	Enabled() bool
}

type CaffeinateWidgetOptions struct {
	Key     streamdeck.KeyID
	Size    int
	Now     func() time.Time
	Backend CaffeinateBackend
}

type CaffeinateWidget struct {
	key     streamdeck.KeyID
	now     func() time.Time
	backend CaffeinateBackend
	source  *caffeinateSource
}

type caffeinateSource struct {
	size  int
	now   func() time.Time
	state CaffeinateBackend
	faces caffeinateFaces

	mu         sync.RWMutex
	pressToken uint64
	pressed    bool
	pressStart time.Time
}

type caffeinateFaces struct {
	title  font.Face
	status font.Face
	hint   font.Face
}

type commandCaffeinateBackend struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

var (
	caffeinateFacesMu    sync.Mutex
	caffeinateFacesCache = map[int]caffeinateFaces{}
)

func NewCaffeinateWidget(options CaffeinateWidgetOptions) (*CaffeinateWidget, error) {
	if options.Size <= 0 {
		options.Size = DefaultClockWidgetSize
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Backend == nil {
		options.Backend = &commandCaffeinateBackend{}
	}

	faces, err := loadCaffeinateFaces(options.Size)
	if err != nil {
		return nil, err
	}

	return &CaffeinateWidget{
		key:     options.Key,
		now:     options.Now,
		backend: options.Backend,
		source: &caffeinateSource{
			size:  options.Size,
			now:   options.Now,
			state: options.Backend,
			faces: faces,
		},
	}, nil
}

func (w *CaffeinateWidget) Button() deckbutton.Button {
	return deckbutton.Button{
		Key: w.key,
		Animation: &deckbutton.Animation{
			Source:    w.source,
			FrameRate: caffeinateWidgetFrameRate,
			Loop:      true,
		},
		OnPress: func(_ *streamdeck.Device, k *streamdeck.Key) error {
			return w.handlePress(k)
		},
	}
}

func (w *CaffeinateWidget) handlePress(k *streamdeck.Key) error {
	token := w.source.beginPress(w.now())

	resultCh := make(chan longPressResult, 1)
	cancelCh := make(chan struct{})
	go func() {
		timer := time.NewTimer(w.source.fillTriggerDuration())
		defer timer.Stop()

		select {
		case <-timer.C:
			if !w.source.triggerLongPress(token) {
				resultCh <- longPressResult{}
				return
			}
			resultCh <- longPressResult{
				triggered: true,
				err:       w.sleepNow(),
			}
		case <-cancelCh:
			resultCh <- longPressResult{}
		}
	}()

	duration := k.WaitForRelease()
	close(cancelCh)
	result := <-resultCh

	if result.triggered {
		w.source.endPress(token)
		return result.err
	}

	if duration >= w.source.fillTriggerDuration() && w.source.triggerLongPress(token) {
		err := w.sleepNow()
		w.source.endPress(token)
		return err
	}

	w.source.endPress(token)
	if shouldToggleOnRelease(duration) {
		return w.toggle()
	}
	return nil
}

func (w *CaffeinateWidget) toggle() error {
	if w.backend.Enabled() {
		return w.backend.Disable()
	}
	return w.backend.Enable()
}

func (w *CaffeinateWidget) sleepNow() error {
	if w.backend.Enabled() {
		if err := w.backend.Disable(); err != nil {
			return err
		}
	}
	return w.backend.Sleep()
}

type longPressResult struct {
	triggered bool
	err       error
}

func (s *caffeinateSource) Start(context.Context) error {
	return nil
}

func (s *caffeinateSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	img := image.NewRGBA(image.Rect(0, 0, s.size, s.size))
	s.render(img)
	return img, nil
}

func (s *caffeinateSource) Duration() time.Duration {
	return 0
}

func (s *caffeinateSource) Close() error {
	if s.state == nil {
		return nil
	}
	return s.state.Disable()
}

func (s *caffeinateSource) beginPress(now time.Time) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pressToken++
	s.pressed = true
	s.pressStart = now
	return s.pressToken
}

func (s *caffeinateSource) triggerLongPress(token uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pressed && s.pressToken == token
}

func (s *caffeinateSource) endPress(token uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pressToken != token {
		return
	}
	s.pressed = false
}

func (s *caffeinateSource) holdProgress(now time.Time) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.pressed {
		return 0
	}
	elapsed := now.Sub(s.pressStart)
	if elapsed <= caffeinateTapDuration {
		return 0
	}
	visibleDuration := caffeinateGaugeDuration - caffeinateTapDuration
	if visibleDuration <= 0 {
		return 1
	}
	progress := float64(elapsed-caffeinateTapDuration) / float64(visibleDuration)
	if progress < 0 {
		return 0
	}
	if progress > 1 {
		return 1
	}
	return progress
}

func (s *caffeinateSource) fillTriggerDuration() time.Duration {
	rawThreshold := gaugeFilledRawProgress(s.size)
	visibleDuration := caffeinateGaugeDuration - caffeinateTapDuration
	return caffeinateTapDuration + time.Duration(math.Ceil(rawThreshold*float64(visibleDuration)))
}

func (s *caffeinateSource) render(dst *image.RGBA) {
	progress := s.holdProgress(s.now())
	displayProgress := easeOutProgress(progress)
	isEnabled := false
	if s.state != nil {
		isEnabled = s.state.Enabled()
	}
	fillSolid(dst, color.RGBA{R: 14, G: 16, B: 18, A: 255})

	if displayProgress > 0 {
		fillStartY := int(math.Round(float64(s.size) * (1 - displayProgress)))
		fillRect(dst, 0, fillStartY, s.size, s.size, color.RGBA{R: 255, G: 171, B: 72, A: 255})
	}

	centerX := float64(s.size) / 2
	drawCenteredText(dst, s.faces.title, "CAFFEINATE", centerX, centeredTextBaselineY(s.faces.title, float64(s.size)*0.24), color.RGBA{R: 227, G: 231, B: 236, A: 255})
	status := "OFF"
	statusColor := color.RGBA{R: 171, G: 178, B: 186, A: 255}
	if isEnabled {
		status = "ON"
		statusColor = color.RGBA{R: 105, G: 233, B: 137, A: 255}
	}
	drawCenteredText(dst, s.faces.status, status, centerX, centeredTextBaselineY(s.faces.status, float64(s.size)*0.53), statusColor)
}

func easeOutProgress(progress float64) float64 {
	if progress <= 0 {
		return 0
	}
	if progress >= 1 {
		return 1
	}
	return 1 - math.Pow(1-progress, caffeinateGaugeEaseExp)
}

func gaugeFilledRawProgress(size int) float64 {
	if size <= 0 {
		return 1
	}
	displayThreshold := 1 - (0.49 / float64(size))
	raw := 1 - math.Pow(1-displayThreshold, 1/caffeinateGaugeEaseExp)
	if raw < 0 {
		return 0
	}
	if raw > 1 {
		return 1
	}
	return raw
}

func shouldToggleOnRelease(duration time.Duration) bool {
	return duration <= caffeinateTapDuration
}

func loadCaffeinateFaces(size int) (caffeinateFaces, error) {
	caffeinateFacesMu.Lock()
	defer caffeinateFacesMu.Unlock()

	if faces, ok := caffeinateFacesCache[size]; ok {
		return faces, nil
	}

	scale := float64(size) / 72.0
	title, err := newFace(gobold.TTF, 7.5*scale)
	if err != nil {
		return caffeinateFaces{}, fmt.Errorf("load caffeinate title font: %w", err)
	}
	status, err := newFace(gobold.TTF, 18*scale)
	if err != nil {
		return caffeinateFaces{}, fmt.Errorf("load caffeinate status font: %w", err)
	}
	hint, err := newFace(gomono.TTF, 6.5*scale)
	if err != nil {
		return caffeinateFaces{}, fmt.Errorf("load caffeinate hint font: %w", err)
	}

	faces := caffeinateFaces{
		title:  title,
		status: status,
		hint:   hint,
	}
	caffeinateFacesCache[size] = faces
	return faces, nil
}

func (b *commandCaffeinateBackend) Enable() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cmd != nil {
		return nil
	}

	cmd := exec.Command("/usr/bin/caffeinate", "-di")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start caffeinate: %w", err)
	}
	b.cmd = cmd
	return nil
}

func (b *commandCaffeinateBackend) Disable() error {
	b.mu.Lock()
	cmd := b.cmd
	b.cmd = nil
	b.mu.Unlock()

	if cmd == nil {
		return nil
	}
	if cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil {
		return fmt.Errorf("stop caffeinate: %w", err)
	}
	_ = cmd.Wait()
	return nil
}

func (b *commandCaffeinateBackend) Sleep() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", `tell application "System Events" to sleep`)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sleep system: %w", err)
	}
	return nil
}

func (b *commandCaffeinateBackend) Enabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cmd != nil
}
