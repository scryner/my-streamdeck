package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/decktouch"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"rafaelmartins.com/p/streamdeck"
)

const (
	playTouchUpdateInterval = time.Second
	playStateCacheTTL       = 500 * time.Millisecond
	playCommandTimeout      = 2 * time.Second
	playSkipDebounceWindow  = 200 * time.Millisecond
	defaultPlayerLabel      = "Playback"
)

var (
	readSystemPlayerState  = readSystemPlaybackState
	sendSystemPlayerToggle = sendSystemPlayPause
	sendSystemPlayerNext   = sendSystemNextTrack
	sendSystemPlayerPrev   = sendSystemPreviousTrack
)

type playerPlaybackState int

const (
	playerPlaybackStateUnknown playerPlaybackState = iota
	playerPlaybackStatePlaying
	playerPlaybackStatePaused
)

type PlayTouchWidgetOptions struct {
	ID     decktouch.WidgetID
	Size   image.Point
	Player PlayerBackend
}

type PlayerBackend interface {
	State(ctx context.Context) (playerPlaybackState, error)
	Toggle(ctx context.Context) error
	Next(ctx context.Context) error
	Previous(ctx context.Context) error
}

type PlayTouchWidget struct {
	touch  decktouch.Widget
	source *playTouchSource

	mu               sync.Mutex
	lastSkipAt       time.Time
	lastSkipStepsDir int
}

type playerVisualMode int

const (
	playerVisualModePlayPause playerVisualMode = iota
	playerVisualModeNext
	playerVisualModePrevious
)

type playTouchSource struct {
	size   image.Point
	player PlayerBackend
	faces  playTouchFaces

	mu         sync.Mutex
	visualMode playerVisualMode
	resetSeq   uint64

	updates chan struct{}
}

type playTouchFaces struct {
	label font.Face
}

type playerSystemBackend struct {
	mu          sync.Mutex
	cachedState playerPlaybackState
	fetchedAt   time.Time

	readState func(context.Context) (playerPlaybackState, error)
	toggle    func(context.Context) error
	next      func(context.Context) error
	previous  func(context.Context) error
}

func NewPlayTouchWidget(options PlayTouchWidgetOptions) (*PlayTouchWidget, error) {
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
	if options.Player == nil {
		options.Player = newPlayerSystemBackend()
	}

	faces, err := loadPlayTouchFaces(options.Size)
	if err != nil {
		return nil, err
	}

	source := &playTouchSource{
		size:    options.Size,
		player:  options.Player,
		faces:   faces,
		updates: make(chan struct{}, 1),
	}

	touch.Animation = &decktouch.Animation{
		Source:         source,
		UpdateInterval: playTouchUpdateInterval,
		Loop:           true,
	}

	widget := &PlayTouchWidget{
		touch:  touch,
		source: source,
	}
	widget.touch.OnTouch = widget.onTouch
	widget.touch.OnDialPress = widget.onDialPress
	widget.touch.OnDialRotate = widget.onDialRotate

	return widget, nil
}

func (w *PlayTouchWidget) Touch() decktouch.Widget {
	return w.touch
}

func (w *PlayTouchWidget) onTouch(_ *streamdeck.Device, _ *decktouch.Widget, typ streamdeck.TouchStripTouchType, _ image.Point) error {
	if typ != streamdeck.TOUCH_STRIP_TOUCH_TYPE_SHORT {
		return nil
	}
	return w.toggle()
}

func (w *PlayTouchWidget) onDialPress(_ *streamdeck.Device, _ *decktouch.Widget, _ *streamdeck.Dial) error {
	return w.toggle()
}

func (w *PlayTouchWidget) onDialRotate(_ *streamdeck.Device, _ *decktouch.Widget, _ *streamdeck.Dial, steps int) error {
	if steps == 0 {
		return nil
	}

	dir := 1
	if steps < 0 {
		dir = -1
	}

	now := time.Now()
	w.mu.Lock()
	if w.lastSkipStepsDir == dir && !w.lastSkipAt.IsZero() && now.Sub(w.lastSkipAt) < playSkipDebounceWindow {
		w.mu.Unlock()
		return nil
	}
	w.lastSkipStepsDir = dir
	w.lastSkipAt = now
	w.mu.Unlock()

	if dir > 0 {
		w.source.setTransientVisualMode(playerVisualModeNext, playSkipDebounceWindow)
	} else {
		w.source.setTransientVisualMode(playerVisualModePrevious, playSkipDebounceWindow)
	}
	w.dispatchSkip(dir)
	return nil
}

func (w *PlayTouchWidget) dispatchSkip(dir int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), playCommandTimeout)
		defer cancel()

		var err error
		if dir > 0 {
			err = w.source.player.Next(ctx)
		} else {
			err = w.source.player.Previous(ctx)
		}
		if err != nil {
			return
		}

		w.source.notify()
	}()
}

func (w *PlayTouchWidget) toggle() error {
	ctx, cancel := context.WithTimeout(context.Background(), playCommandTimeout)
	defer cancel()

	if err := w.source.player.Toggle(ctx); err != nil {
		return err
	}
	w.source.notify()
	return nil
}

func (s *playTouchSource) Start(context.Context) error {
	return nil
}

func (s *playTouchSource) FrameAt(context.Context, time.Duration) (image.Image, error) {
	img := image.NewRGBA(image.Rect(0, 0, s.size.X, s.size.Y))
	s.render(img)
	return img, nil
}

func (s *playTouchSource) Duration() time.Duration {
	return 0
}

func (s *playTouchSource) Updates() <-chan struct{} {
	return s.updates
}

func (s *playTouchSource) Close() error {
	return closeFaces(s.faces.label)
}

func (s *playTouchSource) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

func (s *playTouchSource) setTransientVisualMode(mode playerVisualMode, duration time.Duration) {
	s.mu.Lock()
	s.visualMode = mode
	s.resetSeq++
	seq := s.resetSeq
	s.mu.Unlock()

	s.notify()

	time.AfterFunc(duration, func() {
		s.mu.Lock()
		if s.resetSeq != seq {
			s.mu.Unlock()
			return
		}
		s.visualMode = playerVisualModePlayPause
		s.mu.Unlock()
		s.notify()
	})
}

func (s *playTouchSource) currentVisualMode() playerVisualMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.visualMode
}

func newPlayerSystemBackend() *playerSystemBackend {
	return &playerSystemBackend{
		readState: readSystemPlayerState,
		toggle:    sendSystemPlayerToggle,
		next:      sendSystemPlayerNext,
		previous:  sendSystemPlayerPrev,
	}
}

func (b *playerSystemBackend) State(ctx context.Context) (playerPlaybackState, error) {
	now := time.Now()

	b.mu.Lock()
	if !b.fetchedAt.IsZero() && now.Sub(b.fetchedAt) < playStateCacheTTL {
		state := b.cachedState
		b.mu.Unlock()
		return state, nil
	}
	b.mu.Unlock()

	state, err := b.readState(ctx)
	if err != nil {
		return playerPlaybackStateUnknown, err
	}

	b.mu.Lock()
	b.cachedState = state
	b.fetchedAt = now
	b.mu.Unlock()
	return state, nil
}

func (b *playerSystemBackend) Toggle(ctx context.Context) error {
	if err := b.toggle(ctx); err != nil {
		return fmt.Errorf("toggle play pause: %w", err)
	}
	b.invalidate()
	return nil
}

func (b *playerSystemBackend) Next(ctx context.Context) error {
	if err := b.next(ctx); err != nil {
		return fmt.Errorf("next track: %w", err)
	}
	b.invalidate()
	return nil
}

func (b *playerSystemBackend) Previous(ctx context.Context) error {
	if err := b.previous(ctx); err != nil {
		return fmt.Errorf("previous track: %w", err)
	}
	b.invalidate()
	return nil
}

func (b *playerSystemBackend) invalidate() {
	b.mu.Lock()
	b.fetchedAt = time.Time{}
	b.mu.Unlock()
}

func loadPlayTouchFaces(size image.Point) (playTouchFaces, error) {
	scale := math.Min(float64(size.X)/200.0, float64(size.Y)/100.0)

	label, err := newHangulCapableFace(16 * scale)
	if err != nil {
		label, err = newFace(gobold.TTF, 16*scale)
		if err != nil {
			return playTouchFaces{}, fmt.Errorf("load play touch label font: %w", err)
		}
	}

	return playTouchFaces{label: label}, nil
}

func (s *playTouchSource) render(dst *image.RGBA) {
	bounds := dst.Bounds()
	drawCenteredText(dst, s.faces.label, defaultPlayerLabel, float64(dst.Bounds().Dx())/2, 20, color.RGBA{R: 248, G: 248, B: 249, A: 255})

	iconWidth := int(math.Round(96 * 0.49))
	iconHeight := int(math.Round(58 * 0.49))
	iconX := bounds.Min.X + (bounds.Dx()-iconWidth)/2
	iconY := 54
	switch s.currentVisualMode() {
	case playerVisualModeNext:
		drawNextIcon(dst, iconX, iconY, iconWidth, iconHeight, color.RGBA{R: 248, G: 248, B: 249, A: 255})
	case playerVisualModePrevious:
		drawPreviousIcon(dst, iconX, iconY, iconWidth, iconHeight, color.RGBA{R: 248, G: 248, B: 249, A: 255})
	default:
		drawPlayPauseComboIcon(dst, iconX, iconY, iconWidth, iconHeight, color.RGBA{R: 248, G: 248, B: 249, A: 255})
	}
}

func drawPlayIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	sz := float64(size)
	points := []polygonPoint{
		{x: float64(x), y: float64(y)},
		{x: float64(x), y: float64(y) + sz},
		{x: float64(x) + sz*0.78, y: float64(y) + sz*0.5},
	}
	drawPolygonFill(dst, points, c)
}

func drawPauseIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	barWidth := int(math.Round(float64(size) * 0.24))
	gap := int(math.Round(float64(size) * 0.20))
	height := size
	drawVolumeRoundedRect(dst, x, y, barWidth, height, c)
	drawVolumeRoundedRect(dst, x+barWidth+gap, y, barWidth, height, c)
}

func drawPlayPauseComboIcon(dst *image.RGBA, x, y, width, height int, c color.RGBA) {
	playSize := height
	drawPlayIcon(dst, x, y, playSize, c)

	barWidth := int(math.Round(float64(height) * 0.18))
	gap := int(math.Round(float64(height) * 0.14))
	pauseX := x + playSize + int(math.Round(float64(width-playSize-(barWidth*2)-gap)/2))
	if pauseX < x+playSize+4 {
		pauseX = x + playSize + 4
	}
	drawVolumeRoundedRect(dst, pauseX, y, barWidth, height, c)
	drawVolumeRoundedRect(dst, pauseX+barWidth+gap, y, barWidth, height, c)
}

func drawNextIcon(dst *image.RGBA, x, y, width, height int, c color.RGBA) {
	playSize := height
	gap := max(2, int(math.Round(float64(height)*0.08)))
	barWidth := max(2, int(math.Round(float64(height)*0.14)))

	firstPlayX := x
	secondPlayX := firstPlayX + int(math.Round(float64(playSize)*0.52))
	barX := x + width - barWidth

	if secondPlayX+playSize > barX-gap {
		secondPlayX = barX - gap - playSize
	}
	drawPlayIcon(dst, firstPlayX, y, playSize, c)
	drawPlayIcon(dst, secondPlayX, y, playSize, c)
	drawVolumeRoundedRect(dst, barX, y, barWidth, height, c)
}

func drawPreviousIcon(dst *image.RGBA, x, y, width, height int, c color.RGBA) {
	barWidth := max(2, int(math.Round(float64(height)*0.14)))
	drawVolumeRoundedRect(dst, x, y, barWidth, height, c)

	playSize := height
	gap := max(2, int(math.Round(float64(height)*0.08)))
	firstPlayX := x + barWidth + gap
	secondPlayX := firstPlayX + int(math.Round(float64(playSize)*0.52))
	maxSecondX := x + width - playSize
	if secondPlayX > maxSecondX {
		secondPlayX = maxSecondX
	}
	drawLeftPlayIcon(dst, firstPlayX, y, playSize, c)
	drawLeftPlayIcon(dst, secondPlayX, y, playSize, c)
}

func drawLeftPlayIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	sz := float64(size)
	points := []polygonPoint{
		{x: float64(x) + sz*0.78, y: float64(y)},
		{x: float64(x) + sz*0.78, y: float64(y) + sz},
		{x: float64(x), y: float64(y) + sz*0.5},
	}
	drawPolygonFill(dst, points, c)
}
