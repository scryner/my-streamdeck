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
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/text/unicode/norm"
	"rafaelmartins.com/p/streamdeck"
)

const (
	defaultTouchWidgetWidth     = 200
	defaultTouchWidgetHeight    = 100
	volumeTouchUpdateInterval   = time.Second
	volumeStateCacheTTL         = 500 * time.Millisecond
	volumeSourceCacheTTL        = 30 * time.Second
	volumeCommandTimeout        = 3 * time.Second
	volumeTouchChangeStep       = 4
	defaultUnknownOutputName    = "Unknown Output"
	defaultVolumeOutputNameTrim = "..."
)

var startSystemVolumeObserver = startVolumeObserver
var (
	readSystemVolumeState  = readVolumeState
	readSystemOutputSource = readOutputSourceName
	setSystemOutputVolume  = setOutputVolume
	setSystemOutputMuted   = setOutputMuted
)

type VolumeTouchWidgetOptions struct {
	ID    decktouch.WidgetID
	Size  image.Point
	Audio VolumeBackend
}

type VolumeBackend interface {
	State(ctx context.Context) (VolumeState, error)
	SetVolume(ctx context.Context, percent int) error
	ToggleMute(ctx context.Context) error
}

type VolumeState struct {
	Source string
	Volume int
	Muted  bool
}

type VolumeTouchWidget struct {
	touch  decktouch.Widget
	source *volumeTouchSource
}

type volumeTouchSource struct {
	size  image.Point
	audio VolumeBackend
	faces volumeTouchFaces

	updates chan struct{}
}

type volumeTouchFaces struct {
	title   font.Face
	percent font.Face
}

type audioSystemBackend struct {
	mu sync.Mutex

	cachedState     VolumeState
	stateFetchedAt  time.Time
	sourceFetchedAt time.Time
	stopObserver    func()

	startObserver func(func()) (func(), error)
	readState     func(context.Context) (VolumeState, error)
	readSource    func(context.Context) (string, error)
	setVolume     func(context.Context, int) error
	setMuted      func(context.Context, bool) error
	normalizeName func(string) string
}

type volumeSystemBackend = audioSystemBackend

type volumeBackendWithChangeHandler interface {
	SetChangeHandler(func()) error
}

type volumeBackendWithClose interface {
	Close() error
}

func NewVolumeTouchWidget(options VolumeTouchWidgetOptions) (*VolumeTouchWidget, error) {
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
	if options.Audio == nil {
		options.Audio = newVolumeSystemBackend()
	}

	faces, err := loadVolumeTouchFaces(options.Size)
	if err != nil {
		return nil, err
	}

	source := &volumeTouchSource{
		size:    options.Size,
		audio:   options.Audio,
		faces:   faces,
		updates: make(chan struct{}, 1),
	}

	touch.Animation = &decktouch.Animation{
		Source:         source,
		UpdateInterval: volumeTouchUpdateInterval,
		Loop:           true,
	}

	widget := &VolumeTouchWidget{
		touch:  touch,
		source: source,
	}

	widget.touch.OnTouch = widget.onTouch
	widget.touch.OnDialPress = widget.onDialPress
	widget.touch.OnDialRotate = widget.onDialRotate

	return widget, nil
}

func (w *VolumeTouchWidget) Touch() decktouch.Widget {
	return w.touch
}

func (w *VolumeTouchWidget) onTouch(_ *streamdeck.Device, _ *decktouch.Widget, typ streamdeck.TouchStripTouchType, _ image.Point) error {
	if typ != streamdeck.TOUCH_STRIP_TOUCH_TYPE_SHORT {
		return nil
	}
	return w.toggleMute()
}

func (w *VolumeTouchWidget) onDialPress(_ *streamdeck.Device, _ *decktouch.Widget, _ *streamdeck.Dial) error {
	return w.toggleMute()
}

func (w *VolumeTouchWidget) onDialRotate(_ *streamdeck.Device, _ *decktouch.Widget, _ *streamdeck.Dial, steps int) error {
	if steps == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), volumeCommandTimeout)
	defer cancel()

	state, err := w.source.audio.State(ctx)
	if err != nil {
		return err
	}
	target := clampVolumePercent(state.Volume + steps*volumeTouchChangeStep)
	if err := w.source.audio.SetVolume(ctx, target); err != nil {
		return err
	}
	w.source.notify()
	return nil
}

func (w *VolumeTouchWidget) toggleMute() error {
	ctx, cancel := context.WithTimeout(context.Background(), volumeCommandTimeout)
	defer cancel()
	if err := w.source.audio.ToggleMute(ctx); err != nil {
		return err
	}
	w.source.notify()
	return nil
}

func (s *volumeTouchSource) Start(context.Context) error {
	if backend, ok := s.audio.(volumeBackendWithChangeHandler); ok {
		return backend.SetChangeHandler(s.notify)
	}
	return nil
}

func (s *volumeTouchSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	state, err := s.audio.State(ctx)
	if err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, s.size.X, s.size.Y))
	s.render(img, state)
	return img, nil
}

func (s *volumeTouchSource) Duration() time.Duration {
	return 0
}

func (s *volumeTouchSource) Updates() <-chan struct{} {
	return s.updates
}

func (s *volumeTouchSource) Close() error {
	err := closeFaces(s.faces.title, s.faces.percent)
	if backend, ok := s.audio.(volumeBackendWithClose); ok {
		if closeErr := backend.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func (s *volumeTouchSource) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

func newVolumeSystemBackend() *volumeSystemBackend {
	return &volumeSystemBackend{
		startObserver: startSystemVolumeObserver,
		readState:     readSystemVolumeState,
		readSource:    readSystemOutputSource,
		setVolume:     setSystemOutputVolume,
		setMuted:      setSystemOutputMuted,
		normalizeName: normalizeAudioSourceName,
	}
}

func (b *audioSystemBackend) SetChangeHandler(fn func()) error {
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

func (b *audioSystemBackend) Close() error {
	b.mu.Lock()
	stop := b.stopObserver
	b.stopObserver = nil
	b.mu.Unlock()
	if stop != nil {
		stop()
	}
	return nil
}

func (b *audioSystemBackend) invalidateStateCache() {
	b.mu.Lock()
	b.stateFetchedAt = time.Time{}
	b.sourceFetchedAt = time.Time{}
	b.mu.Unlock()
}

func (b *audioSystemBackend) State(ctx context.Context) (VolumeState, error) {
	now := time.Now()

	b.mu.Lock()
	state := b.cachedState
	stateFresh := !b.stateFetchedAt.IsZero() && now.Sub(b.stateFetchedAt) < volumeStateCacheTTL
	sourceFresh := state.Source != "" && !b.sourceFetchedAt.IsZero() && now.Sub(b.sourceFetchedAt) < volumeSourceCacheTTL
	b.mu.Unlock()

	if !stateFresh {
		volumeState, err := b.fetchVolumeState(ctx)
		if err != nil {
			b.mu.Lock()
			defer b.mu.Unlock()
			if !b.stateFetchedAt.IsZero() {
				return b.cachedState, nil
			}
			return VolumeState{}, err
		}

		b.mu.Lock()
		b.cachedState.Volume = volumeState.Volume
		b.cachedState.Muted = volumeState.Muted
		b.stateFetchedAt = now
		state = b.cachedState
		b.mu.Unlock()
	}

	if !sourceFresh {
		sourceName, err := b.fetchAudioSource(ctx)
		if err == nil && sourceName != "" {
			b.mu.Lock()
			b.cachedState.Source = sourceName
			b.sourceFetchedAt = now
			state = b.cachedState
			b.mu.Unlock()
		}
	}

	if strings.TrimSpace(state.Source) == "" {
		state.Source = defaultUnknownOutputName
	}
	return state, nil
}

func (b *audioSystemBackend) SetVolume(ctx context.Context, percent int) error {
	percent = clampVolumePercent(percent)
	if err := b.setVolume(ctx, percent); err != nil {
		return fmt.Errorf("set output volume: %w", err)
	}

	b.mu.Lock()
	b.cachedState.Volume = percent
	b.stateFetchedAt = time.Now()
	b.mu.Unlock()
	return nil
}

func (b *audioSystemBackend) ToggleMute(ctx context.Context) error {
	state, err := b.State(ctx)
	if err != nil {
		return err
	}

	if err := b.setMuted(ctx, !state.Muted); err != nil {
		return fmt.Errorf("toggle output mute: %w", err)
	}

	b.mu.Lock()
	b.cachedState.Muted = !state.Muted
	b.stateFetchedAt = time.Now()
	b.mu.Unlock()
	return nil
}

func (b *audioSystemBackend) fetchVolumeState(ctx context.Context) (VolumeState, error) {
	return b.readState(ctx)
}

func (b *audioSystemBackend) fetchAudioSource(ctx context.Context) (string, error) {
	name, err := b.readSource(ctx)
	if err != nil {
		return "", err
	}
	if b.normalizeName != nil {
		return b.normalizeName(name), nil
	}
	return normalizeAudioSourceName(name), nil
}

func normalizeAudioSourceName(name string) string {
	name = strings.ReplaceAll(name, "\u00a0", " ")
	name = norm.NFC.String(name)
	name = strings.TrimSpace(name)

	suffixes := []string{
		" 스피커",
		" Speakers",
		" Speaker",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSpace(strings.TrimSuffix(name, suffix))
			break
		}
	}
	if name == "" {
		return defaultUnknownOutputName
	}
	return name
}

func loadVolumeTouchFaces(size image.Point) (volumeTouchFaces, error) {
	scale := math.Min(float64(size.X)/200.0, float64(size.Y)/100.0)

	title, err := newHangulCapableFace(16 * scale)
	if err != nil {
		title, err = newFace(gobold.TTF, 16*scale)
		if err != nil {
			return volumeTouchFaces{}, fmt.Errorf("load volume touch title font: %w", err)
		}
	}
	percent, err := newFace(gobold.TTF, 29*scale)
	if err != nil {
		_ = title.Close()
		return volumeTouchFaces{}, fmt.Errorf("load volume touch percent font: %w", err)
	}

	return volumeTouchFaces{
		title:   title,
		percent: percent,
	}, nil
}

func (s *volumeTouchSource) render(dst *image.RGBA, state VolumeState) {
	sourceText := ellipsizeText(s.faces.title, state.Source, float64(dst.Bounds().Dx()-12))
	drawCenteredText(dst, s.faces.title, sourceText, float64(dst.Bounds().Dx())/2, 20, color.RGBA{R: 248, G: 248, B: 249, A: 255})

	iconColor := color.RGBA{R: 248, G: 248, B: 249, A: 255}
	drawVolumeSpeakerIcon(dst, 18, 47, 34, iconColor, state.Muted)

	percentText := fmt.Sprintf("%d%%", clampVolumePercent(state.Volume))
	drawCenteredText(dst, s.faces.percent, percentText, float64(dst.Bounds().Dx()-46), centeredTextBaselineY(s.faces.percent, 50), color.RGBA{R: 248, G: 248, B: 249, A: 255})

	trackX := 72
	trackY := 80
	trackWidth := dst.Bounds().Dx() - trackX - 10
	trackHeight := 12
	drawVolumeSlider(dst, trackX, trackY, trackWidth, trackHeight, float64(clampVolumePercent(state.Volume))/100.0)
}

func ellipsizeText(face font.Face, text string, maxWidth float64) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultUnknownOutputName
	}
	if measureTextWidth(face, text) <= maxWidth {
		return text
	}

	runes := []rune(text)
	for len(runes) > 0 {
		candidate := string(runes) + defaultVolumeOutputNameTrim
		if measureTextWidth(face, candidate) <= maxWidth {
			return candidate
		}
		runes = runes[:len(runes)-1]
	}
	return defaultVolumeOutputNameTrim
}

func drawVolumeSpeakerIcon(dst *image.RGBA, x, y, size int, c color.RGBA, muted bool) {
	sz := float64(size)
	body := []polygonPoint{
		{x: float64(x), y: float64(y) + sz*0.35},
		{x: float64(x) + sz*0.18, y: float64(y) + sz*0.35},
		{x: float64(x) + sz*0.48, y: float64(y) + sz*0.08},
		{x: float64(x) + sz*0.48, y: float64(y) + sz*0.92},
		{x: float64(x) + sz*0.18, y: float64(y) + sz*0.65},
		{x: float64(x), y: float64(y) + sz*0.65},
	}
	drawPolygonFill(dst, body, c)

	centerX := float64(x) + sz*0.52
	centerY := float64(y) + sz*0.5
	lineWidth := math.Max(2.5, sz*0.08)
	if muted {
		muteCross := color.RGBA{R: 255, G: 92, B: 92, A: 255}
		drawLineWidth(dst, centerX+sz*0.08, centerY-sz*0.26, centerX+sz*0.36, centerY+sz*0.26, lineWidth, muteCross)
		drawLineWidth(dst, centerX+sz*0.36, centerY-sz*0.26, centerX+sz*0.08, centerY+sz*0.26, lineWidth, muteCross)
		return
	}

	drawEllipseArc(dst, centerX+sz*0.14, centerY, sz*0.15, sz*0.24, -0.38*math.Pi, 0.38*math.Pi, lineWidth, c)
	drawEllipseArc(dst, centerX+sz*0.28, centerY, sz*0.27, sz*0.40, -0.38*math.Pi, 0.38*math.Pi, lineWidth, c)
}

func drawVolumeSlider(dst *image.RGBA, x, y, width, height int, progress float64) {
	if width <= 0 || height <= 0 {
		return
	}

	bg := color.RGBA{R: 74, G: 79, B: 86, A: 255}
	fg := color.RGBA{R: 248, G: 248, B: 249, A: 255}
	drawVolumeRoundedRect(dst, x, y, width, height, bg)

	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}

	fillWidth := int(math.Round(float64(width) * progress))
	if fillWidth <= 0 {
		return
	}
	if fillWidth > width {
		fillWidth = width
	}
	drawVolumeRoundedRect(dst, x, y, fillWidth, height, fg)
}

func clampVolumePercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func drawVolumeRoundedRect(dst *image.RGBA, x, y, width, height int, c color.RGBA) {
	if width <= 0 || height <= 0 {
		return
	}

	radius := math.Min(float64(height)/2, float64(width)/2)
	for yy := y; yy < y+height; yy++ {
		for xx := x; xx < x+width; xx++ {
			if pointInVolumeRoundedRect(float64(xx)+0.5, float64(yy)+0.5, float64(x), float64(y), float64(width), float64(height), radius) {
				dst.SetRGBA(xx, yy, c)
			}
		}
	}
}

func pointInVolumeRoundedRect(px, py, x, y, width, height, radius float64) bool {
	if radius <= 0 {
		return px >= x && px <= x+width && py >= y && py <= y+height
	}

	left := x + radius
	right := x + width - radius
	top := y + radius
	bottom := y + height - radius

	if px >= left && px <= right && py >= y && py <= y+height {
		return true
	}
	if py >= top && py <= bottom && px >= x && px <= x+width {
		return true
	}

	corners := [][2]float64{
		{left, top},
		{right, top},
		{left, bottom},
		{right, bottom},
	}
	r2 := radius * radius
	for _, corner := range corners {
		dx := px - corner[0]
		dy := py - corner[1]
		if dx*dx+dy*dy <= r2 {
			return true
		}
	}
	return false
}
