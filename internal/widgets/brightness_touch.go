package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/decktouch"
	"golang.org/x/text/unicode/norm"
	"rafaelmartins.com/p/streamdeck"
)

const (
	brightnessTouchUpdateInterval = 500 * time.Millisecond
	brightnessStateCacheTTL       = 500 * time.Millisecond
	brightnessDisplayCacheTTL     = 30 * time.Second
	brightnessCommandTimeout      = 3 * time.Second
	brightnessTouchChangeStep     = 4
	defaultUnknownDisplayName     = "Display Brightness"
	defaultBrightnessNoControl    = "NO CONTROL"
)

var (
	startSystemBrightnessObserver = startDisplayObserver
	readSystemBrightnessState     = readMainDisplayBrightness
	readSystemBrightnessDisplay   = readMainDisplayName
	setSystemBrightnessLevel      = setMainDisplayBrightness
)

type BrightnessTouchWidgetOptions struct {
	ID         decktouch.WidgetID
	Size       image.Point
	Brightness BrightnessBackend
}

type BrightnessBackend interface {
	State(ctx context.Context) (BrightnessState, error)
	SetBrightness(ctx context.Context, percent int) error
}

type BrightnessState struct {
	Display    string
	Brightness int
}

type BrightnessTouchWidget struct {
	touch  decktouch.Widget
	source *brightnessTouchSource
}

type brightnessTouchSource struct {
	size       image.Point
	brightness BrightnessBackend
	faces      volumeTouchFaces

	updates chan struct{}
}

type brightnessSystemBackend struct {
	mu sync.Mutex

	cachedState      BrightnessState
	stateFetchedAt   time.Time
	displayFetchedAt time.Time
	stopObserver     func()

	startObserver  func(func()) (func(), error)
	readBrightness func(context.Context) (int, error)
	readDisplay    func(context.Context) (string, error)
	setBrightness  func(context.Context, int) error
}

type brightnessBackendWithChangeHandler interface {
	SetChangeHandler(func()) error
}

type brightnessBackendWithClose interface {
	Close() error
}

func NewBrightnessTouchWidget(options BrightnessTouchWidgetOptions) (*BrightnessTouchWidget, error) {
	touch, err := decktouch.NewWidget(options.ID)
	if err != nil {
		return nil, err
	}
	if options.Size.X <= 0 {
		options.Size.X = defaultTouchWidgetWidth
	}
	if options.Size.Y <= 0 {
		options.Size.Y = defaultTouchWidgetHeight
	}
	if options.Brightness == nil {
		options.Brightness = newBrightnessSystemBackend()
	}

	faces, err := loadVolumeTouchFaces(options.Size)
	if err != nil {
		return nil, err
	}

	source := &brightnessTouchSource{
		size:       options.Size,
		brightness: options.Brightness,
		faces:      faces,
		updates:    make(chan struct{}, 1),
	}

	touch.Animation = &decktouch.Animation{
		Source:         source,
		UpdateInterval: brightnessTouchUpdateInterval,
		Loop:           true,
	}

	widget := &BrightnessTouchWidget{
		touch:  touch,
		source: source,
	}
	widget.touch.OnDialRotate = widget.onDialRotate

	return widget, nil
}

func (w *BrightnessTouchWidget) Touch() decktouch.Widget {
	return w.touch
}

func (w *BrightnessTouchWidget) onDialRotate(_ *streamdeck.Device, _ *decktouch.Widget, _ *streamdeck.Dial, steps int) error {
	if steps == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), brightnessCommandTimeout)
	defer cancel()

	state, err := w.source.brightness.State(ctx)
	if err != nil {
		return err
	}

	target := clampBrightnessPercent(state.Brightness + steps*brightnessTouchChangeStep)
	if err := w.source.brightness.SetBrightness(ctx, target); err != nil {
		return err
	}
	w.source.notify()
	return nil
}

func (s *brightnessTouchSource) Start(context.Context) error {
	if backend, ok := s.brightness.(brightnessBackendWithChangeHandler); ok {
		if err := backend.SetChangeHandler(s.notify); err != nil {
			return nil
		}
	}
	return nil
}

func (s *brightnessTouchSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	img := image.NewRGBA(image.Rect(0, 0, s.size.X, s.size.Y))

	state, err := s.brightness.State(ctx)
	if err != nil {
		s.renderUnavailable(img)
		return img, nil
	}

	s.render(img, state)
	return img, nil
}

func (s *brightnessTouchSource) StateSignature(ctx context.Context, _ time.Duration) (uint64, error) {
	state, err := s.brightness.State(ctx)
	if err != nil {
		sum := newStateHash()
		sum = addStateHashString(sum, defaultBrightnessNoControl)
		return sum, nil
	}

	sum := newStateHash()
	sum = addStateHashString(sum, state.Display)
	sum = addStateHashInt(sum, clampBrightnessPercent(state.Brightness))
	return sum, nil
}

func (s *brightnessTouchSource) Duration() time.Duration {
	return 0
}

func (s *brightnessTouchSource) Updates() <-chan struct{} {
	return s.updates
}

func (s *brightnessTouchSource) Close() error {
	if backend, ok := s.brightness.(brightnessBackendWithClose); ok {
		if err := backend.Close(); err != nil {
			return err
		}
	}
	return closeFaces(s.faces.title, s.faces.percent)
}

func (s *brightnessTouchSource) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

func newBrightnessSystemBackend() *brightnessSystemBackend {
	return &brightnessSystemBackend{
		startObserver:  startSystemBrightnessObserver,
		readBrightness: readSystemBrightnessState,
		readDisplay:    readSystemBrightnessDisplay,
		setBrightness:  setSystemBrightnessLevel,
	}
}

func (b *brightnessSystemBackend) SetChangeHandler(fn func()) error {
	b.mu.Lock()
	stop := b.stopObserver
	b.stopObserver = nil
	b.mu.Unlock()
	if stop != nil {
		stop()
	}
	if fn == nil {
		return nil
	}

	stop, err := b.startObserver(func() {
		b.invalidateStateCache()
		fn()
	})
	if err != nil {
		return err
	}

	b.mu.Lock()
	b.stopObserver = stop
	b.mu.Unlock()
	return nil
}

func (b *brightnessSystemBackend) Close() error {
	b.mu.Lock()
	stop := b.stopObserver
	b.stopObserver = nil
	b.mu.Unlock()
	if stop != nil {
		stop()
	}

	b.mu.Lock()
	b.cachedState = BrightnessState{}
	b.stateFetchedAt = time.Time{}
	b.displayFetchedAt = time.Time{}
	b.mu.Unlock()
	return nil
}

func (b *brightnessSystemBackend) invalidateStateCache() {
	b.mu.Lock()
	b.stateFetchedAt = time.Time{}
	b.displayFetchedAt = time.Time{}
	b.mu.Unlock()
}

func (b *brightnessSystemBackend) State(ctx context.Context) (BrightnessState, error) {
	now := time.Now()

	b.mu.Lock()
	state := b.cachedState
	brightnessFresh := !b.stateFetchedAt.IsZero() && now.Sub(b.stateFetchedAt) < brightnessStateCacheTTL
	displayFresh := state.Display != "" && !b.displayFetchedAt.IsZero() && now.Sub(b.displayFetchedAt) < brightnessDisplayCacheTTL
	b.mu.Unlock()

	if !brightnessFresh {
		brightness, err := b.readBrightness(ctx)
		if err != nil {
			b.mu.Lock()
			defer b.mu.Unlock()
			if !b.stateFetchedAt.IsZero() {
				return b.cachedState, nil
			}
			return BrightnessState{}, err
		}

		b.mu.Lock()
		b.cachedState.Brightness = clampBrightnessPercent(brightness)
		b.stateFetchedAt = now
		state = b.cachedState
		b.mu.Unlock()
	}

	if !displayFresh {
		display, err := b.readDisplay(ctx)
		if err == nil && strings.TrimSpace(display) != "" {
			b.mu.Lock()
			b.cachedState.Display = normalizeDisplayName(display)
			b.displayFetchedAt = now
			state = b.cachedState
			b.mu.Unlock()
		}
	}

	if strings.TrimSpace(state.Display) == "" {
		state.Display = defaultUnknownDisplayName
	}
	return state, nil
}

func (b *brightnessSystemBackend) SetBrightness(ctx context.Context, percent int) error {
	percent = clampBrightnessPercent(percent)
	if err := b.setBrightness(ctx, percent); err != nil {
		return fmt.Errorf("set display brightness: %w", err)
	}

	b.mu.Lock()
	b.cachedState.Brightness = percent
	b.stateFetchedAt = time.Now()
	b.mu.Unlock()
	return nil
}

func normalizeDisplayName(name string) string {
	name = strings.ReplaceAll(name, "\u00a0", " ")
	name = norm.NFC.String(name)
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultUnknownDisplayName
	}
	return name
}

func (s *brightnessTouchSource) render(dst *image.RGBA, state BrightnessState) {
	sourceText := ellipsizeText(s.faces.title, defaultUnknownDisplayName, float64(dst.Bounds().Dx()-12))
	drawCenteredText(dst, s.faces.title, sourceText, float64(dst.Bounds().Dx())/2, 20, color.RGBA{R: 248, G: 248, B: 249, A: 255})

	iconColor := color.RGBA{R: 248, G: 248, B: 249, A: 255}
	drawBrightnessIcon(dst, 18, 47, 34, iconColor)

	percentText := fmt.Sprintf("%d%%", clampBrightnessPercent(state.Brightness))
	drawCenteredText(dst, s.faces.percent, percentText, float64(dst.Bounds().Dx()-46), centeredTextBaselineY(s.faces.percent, 50), color.RGBA{R: 248, G: 248, B: 249, A: 255})

	trackX := 72
	trackY := 80
	trackWidth := dst.Bounds().Dx() - trackX - 10
	trackHeight := 12
	drawVolumeSlider(dst, trackX, trackY, trackWidth, trackHeight, float64(clampBrightnessPercent(state.Brightness))/100.0)
}

func (s *brightnessTouchSource) renderUnavailable(dst *image.RGBA) {
	drawCenteredText(dst, s.faces.title, defaultBrightnessNoControl, float64(dst.Bounds().Dx())/2, centeredTextBaselineY(s.faces.title, float64(dst.Bounds().Dy())/2), color.RGBA{R: 248, G: 248, B: 249, A: 255})
}

func drawBrightnessIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	sz := float64(size)
	centerX := float64(x) + sz*0.5
	centerY := float64(y) + sz*0.5
	coreRadius := sz * 0.18
	rayInner := sz * 0.34
	rayOuter := sz * 0.48
	lineWidth := math.Max(2.4, sz*0.08)

	fillCircle(dst, centerX, centerY, coreRadius, c)
	for _, angle := range []float64{0, math.Pi / 4, math.Pi / 2, 3 * math.Pi / 4, math.Pi, 5 * math.Pi / 4, 3 * math.Pi / 2, 7 * math.Pi / 4} {
		x1 := centerX + math.Cos(angle)*rayInner
		y1 := centerY + math.Sin(angle)*rayInner
		x2 := centerX + math.Cos(angle)*rayOuter
		y2 := centerY + math.Sin(angle)*rayOuter
		drawLineWidth(dst, x1, y1, x2, y2, lineWidth, c)
	}
}

func fillCircle(dst *image.RGBA, centerX, centerY, radius float64, c color.RGBA) {
	minX := int(math.Floor(centerX - radius))
	maxX := int(math.Ceil(centerX + radius))
	minY := int(math.Floor(centerY - radius))
	maxY := int(math.Ceil(centerY + radius))
	r2 := radius * radius

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := (float64(x) + 0.5) - centerX
			dy := (float64(y) + 0.5) - centerY
			if dx*dx+dy*dy <= r2 {
				if image.Pt(x, y).In(dst.Bounds()) {
					dst.SetRGBA(x, y, c)
				}
			}
		}
	}
}

func clampBrightnessPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
