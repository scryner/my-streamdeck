package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"github.com/scryner/my-streamdeck/internal/decktouch"
	"golang.org/x/text/unicode/norm"
	"rafaelmartins.com/p/streamdeck"
)

var (
	startSystemInputObserver = startInputObserver
	readSystemInputState     = readInputVolumeState
	readSystemInputSource    = readInputSourceName
	setSystemInputVolume     = setInputVolume
	setSystemInputMuted      = setInputMuted
)

type MicrophoneBackend = VolumeBackend

type MicrophoneTouchWidgetOptions struct {
	ID    decktouch.WidgetID
	Size  image.Point
	Audio MicrophoneBackend
}

type MicrophoneTouchWidget struct {
	touch  decktouch.Widget
	source *microphoneTouchSource
}

type microphoneTouchSource struct {
	size  image.Point
	audio MicrophoneBackend
	faces volumeTouchFaces

	updates chan struct{}
}

type microphoneSystemBackend = audioSystemBackend

func NewMicrophoneTouchWidget(options MicrophoneTouchWidgetOptions) (*MicrophoneTouchWidget, error) {
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
		options.Audio = newMicrophoneSystemBackend()
	}

	faces, err := loadVolumeTouchFaces(options.Size)
	if err != nil {
		return nil, err
	}

	source := &microphoneTouchSource{
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

	widget := &MicrophoneTouchWidget{
		touch:  touch,
		source: source,
	}

	widget.touch.OnTouch = widget.onTouch
	widget.touch.OnDialPress = widget.onDialPress
	widget.touch.OnDialRotate = widget.onDialRotate

	return widget, nil
}

func (w *MicrophoneTouchWidget) Touch() decktouch.Widget {
	return w.touch
}

func (w *MicrophoneTouchWidget) onTouch(_ *streamdeck.Device, _ *decktouch.Widget, typ streamdeck.TouchStripTouchType, _ image.Point) error {
	if typ != streamdeck.TOUCH_STRIP_TOUCH_TYPE_SHORT {
		return nil
	}
	return w.toggleMute()
}

func (w *MicrophoneTouchWidget) onDialPress(_ *streamdeck.Device, _ *decktouch.Widget, _ *streamdeck.Dial) error {
	return w.toggleMute()
}

func (w *MicrophoneTouchWidget) onDialRotate(_ *streamdeck.Device, _ *decktouch.Widget, _ *streamdeck.Dial, steps int) error {
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

func (w *MicrophoneTouchWidget) toggleMute() error {
	ctx, cancel := context.WithTimeout(context.Background(), volumeCommandTimeout)
	defer cancel()
	if err := w.source.audio.ToggleMute(ctx); err != nil {
		return err
	}
	w.source.notify()
	return nil
}

func (s *microphoneTouchSource) Start(context.Context) error {
	if backend, ok := s.audio.(volumeBackendWithChangeHandler); ok {
		return backend.SetChangeHandler(s.notify)
	}
	return nil
}

func (s *microphoneTouchSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	state, err := s.audio.State(ctx)
	if err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, s.size.X, s.size.Y))
	s.render(img, state)
	return img, nil
}

func (s *microphoneTouchSource) Duration() time.Duration {
	return 0
}

func (s *microphoneTouchSource) Updates() <-chan struct{} {
	return s.updates
}

func (s *microphoneTouchSource) Close() error {
	if backend, ok := s.audio.(volumeBackendWithClose); ok {
		if err := backend.Close(); err != nil {
			return err
		}
	}
	return closeFaces(s.faces.title, s.faces.percent)
}

func (s *microphoneTouchSource) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

func (s *microphoneTouchSource) render(dst *image.RGBA, state VolumeState) {
	sourceText := ellipsizeText(s.faces.title, state.Source, float64(dst.Bounds().Dx()-12))
	drawCenteredText(dst, s.faces.title, sourceText, float64(dst.Bounds().Dx())/2, 20, color.RGBA{R: 248, G: 248, B: 249, A: 255})

	iconColor := color.RGBA{R: 248, G: 248, B: 249, A: 255}
	drawMicrophoneIcon(dst, 18, 47, 34, iconColor, state.Muted)

	percentText := fmt.Sprintf("%d%%", clampVolumePercent(state.Volume))
	drawCenteredText(dst, s.faces.percent, percentText, float64(dst.Bounds().Dx()-46), centeredTextBaselineY(s.faces.percent, 50), color.RGBA{R: 248, G: 248, B: 249, A: 255})

	trackX := 72
	trackY := 80
	trackWidth := dst.Bounds().Dx() - trackX - 10
	trackHeight := 12
	drawVolumeSlider(dst, trackX, trackY, trackWidth, trackHeight, float64(clampVolumePercent(state.Volume))/100.0)
}

func newMicrophoneSystemBackend() *microphoneSystemBackend {
	return &microphoneSystemBackend{
		startObserver: startSystemInputObserver,
		readState:     readSystemInputState,
		readSource:    readSystemInputSource,
		setVolume:     setSystemInputVolume,
		setMuted:      setSystemInputMuted,
		normalizeName: normalizeMicrophoneSourceName,
	}
}

func normalizeMicrophoneSourceName(name string) string {
	name = strings.ReplaceAll(name, "\u00a0", " ")
	name = norm.NFC.String(name)
	name = strings.TrimSpace(name)

	suffixes := []string{
		" 마이크",
		" Microphone",
		" Mic",
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

func drawMicrophoneIcon(dst *image.RGBA, x, y, size int, c color.RGBA, muted bool) {
	sz := float64(size)
	bodyX := float64(x) + sz*0.24
	bodyY := float64(y)
	bodyW := sz * 0.34
	bodyH := sz * 0.58
	drawVolumeRoundedRect(dst, int(math.Round(bodyX)), int(math.Round(bodyY)), int(math.Round(bodyW)), int(math.Round(bodyH)), c)
	innerX := int(math.Round(bodyX + sz*0.08))
	innerY := int(math.Round(bodyY + sz*0.08))
	innerW := int(math.Round(bodyW - sz*0.16))
	innerH := int(math.Round(bodyH - sz*0.16))
	drawVolumeRoundedRect(dst, innerX, innerY, innerW, innerH, color.RGBA{})

	centerX := bodyX + bodyW/2
	bottomY := bodyY + bodyH
	drawEllipseArc(dst, centerX, bottomY-(sz*0.01), sz*0.34, sz*0.30, 0.08*math.Pi, 0.92*math.Pi, math.Max(2.5, sz*0.08), c)
	drawLineWidth(dst, centerX, bottomY+sz*0.04, centerX, bottomY+sz*0.24, math.Max(2.5, sz*0.08), c)
	drawLineWidth(dst, centerX-sz*0.18, bottomY+sz*0.24, centerX+sz*0.18, bottomY+sz*0.24, math.Max(2.5, sz*0.08), c)

	if muted {
		muteSlash := color.RGBA{R: 255, G: 92, B: 92, A: 255}
		drawLineWidth(dst, float64(x)+sz*0.08, float64(y)+sz*0.86, float64(x)+sz*0.84, float64(y)+sz*0.10, math.Max(3.0, sz*0.09), muteSlash)
	}
}
