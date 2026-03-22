package widgets

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/decktouch"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
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

type volumeSystemBackend struct {
	runCommand func(ctx context.Context, name string, args ...string) ([]byte, error)

	mu sync.Mutex

	cachedState     VolumeState
	stateFetchedAt  time.Time
	sourceFetchedAt time.Time
}

type volumeTouchAudioProfile struct {
	SPAudioDataType []struct {
		Items []struct {
			Name                     string `json:"_name"`
			DefaultAudioOutputDevice string `json:"coreaudio_default_audio_output_device"`
		} `json:"_items"`
	} `json:"SPAudioDataType"`
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
	return closeFaces(s.faces.title, s.faces.percent)
}

func (s *volumeTouchSource) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

func newVolumeSystemBackend() *volumeSystemBackend {
	return &volumeSystemBackend{
		runCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
	}
}

func (b *volumeSystemBackend) State(ctx context.Context) (VolumeState, error) {
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
		sourceName, err := b.fetchOutputSource(ctx)
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

func (b *volumeSystemBackend) SetVolume(ctx context.Context, percent int) error {
	percent = clampVolumePercent(percent)

	muted, err := b.currentMutedState(ctx)
	if err != nil {
		b.mu.Lock()
		muted = b.cachedState.Muted
		b.mu.Unlock()
	}

	script := fmt.Sprintf("set volume output volume %d without output muted", percent)
	if muted {
		script = fmt.Sprintf("set volume output volume %d with output muted", percent)
	}
	if _, err := b.runCommand(ctx, "/usr/bin/osascript", "-e", script); err != nil {
		return fmt.Errorf("set output volume: %w", err)
	}

	b.mu.Lock()
	b.cachedState.Volume = percent
	b.cachedState.Muted = muted
	b.stateFetchedAt = time.Now()
	b.mu.Unlock()
	return nil
}

func (b *volumeSystemBackend) currentMutedState(ctx context.Context) (bool, error) {
	now := time.Now()

	b.mu.Lock()
	if !b.stateFetchedAt.IsZero() && now.Sub(b.stateFetchedAt) < volumeStateCacheTTL {
		muted := b.cachedState.Muted
		b.mu.Unlock()
		return muted, nil
	}
	b.mu.Unlock()

	state, err := b.fetchVolumeState(ctx)
	if err != nil {
		return false, err
	}

	b.mu.Lock()
	b.cachedState.Volume = state.Volume
	b.cachedState.Muted = state.Muted
	b.stateFetchedAt = now
	b.mu.Unlock()
	return state.Muted, nil
}

func (b *volumeSystemBackend) ToggleMute(ctx context.Context) error {
	state, err := b.State(ctx)
	if err != nil {
		return err
	}

	script := "set volume with output muted"
	if state.Muted {
		script = "set volume without output muted"
	}
	if _, err := b.runCommand(ctx, "/usr/bin/osascript", "-e", script); err != nil {
		return fmt.Errorf("toggle output mute: %w", err)
	}

	b.mu.Lock()
	b.cachedState.Muted = !state.Muted
	b.stateFetchedAt = time.Now()
	b.mu.Unlock()
	return nil
}

func (b *volumeSystemBackend) fetchVolumeState(ctx context.Context) (VolumeState, error) {
	output, err := b.runCommand(
		ctx,
		"/usr/bin/osascript",
		"-e", "set v to get volume settings",
		"-e", `return (output volume of v as string) & "," & (output muted of v as string)`,
	)
	if err != nil {
		return VolumeState{}, fmt.Errorf("read volume state: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(string(output)), ",")
	if len(parts) != 2 {
		return VolumeState{}, fmt.Errorf("read volume state: unexpected output %q", strings.TrimSpace(string(output)))
	}

	volume, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return VolumeState{}, fmt.Errorf("parse volume: %w", err)
	}

	return VolumeState{
		Volume: clampVolumePercent(volume),
		Muted:  strings.EqualFold(strings.TrimSpace(parts[1]), "true"),
	}, nil
}

func (b *volumeSystemBackend) fetchOutputSource(ctx context.Context) (string, error) {
	output, err := b.runCommand(ctx, "/usr/sbin/system_profiler", "SPAudioDataType", "-json")
	if err != nil {
		return "", fmt.Errorf("read output source: %w", err)
	}

	var profile volumeTouchAudioProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		return "", fmt.Errorf("decode output source: %w", err)
	}

	for _, group := range profile.SPAudioDataType {
		for _, item := range group.Items {
			if item.DefaultAudioOutputDevice == "spaudio_yes" {
				return normalizeOutputSourceName(item.Name), nil
			}
		}
	}

	return "", fmt.Errorf("default output source not found")
}

func normalizeOutputSourceName(name string) string {
	name = strings.ReplaceAll(name, "\u00a0", " ")
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

	title, err := newFace(gobold.TTF, 15*scale)
	if err != nil {
		return volumeTouchFaces{}, fmt.Errorf("load volume touch title font: %w", err)
	}
	percent, err := newFace(gobold.TTF, 27*scale)
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
	drawCenteredText(dst, s.faces.title, sourceText, float64(dst.Bounds().Dx())/2, 19, color.RGBA{R: 248, G: 248, B: 249, A: 255})

	iconColor := color.RGBA{R: 248, G: 248, B: 249, A: 255}
	drawVolumeSpeakerIcon(dst, 18, 30, 42, iconColor, state.Muted)

	percentText := fmt.Sprintf("%d%%", clampVolumePercent(state.Volume))
	drawCenteredText(dst, s.faces.percent, percentText, float64(dst.Bounds().Dx()-48), centeredTextBaselineY(s.faces.percent, 46), color.RGBA{R: 248, G: 248, B: 249, A: 255})

	trackX := 78
	trackY := 74
	trackWidth := dst.Bounds().Dx() - trackX - 12
	trackHeight := 10
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
	body := []polygonPoint{
		{x: float64(x), y: float64(y + size/3)},
		{x: float64(x + size/5), y: float64(y + size/3)},
		{x: float64(x + size/2), y: float64(y)},
		{x: float64(x + size/2), y: float64(y + size)},
		{x: float64(x + size/5), y: float64(y + (size * 2 / 3))},
		{x: float64(x), y: float64(y + (size * 2 / 3))},
	}
	drawPolygonFill(dst, body, c)

	centerX := float64(x + size/2 + 3)
	centerY := float64(y + size/2)
	if muted {
		drawLineWidth(dst, centerX+4, centerY-14, centerX+22, centerY+14, 4, c)
		drawLineWidth(dst, centerX+22, centerY-14, centerX+4, centerY+14, 4, c)
		return
	}

	drawEllipseArc(dst, centerX+10, centerY, 8, 12, -0.38*math.Pi, 0.38*math.Pi, 3, c)
	drawEllipseArc(dst, centerX+19, centerY, 14, 20, -0.38*math.Pi, 0.38*math.Pi, 3, c)
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
