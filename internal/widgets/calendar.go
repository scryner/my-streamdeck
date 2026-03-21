package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"os/exec"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"rafaelmartins.com/p/streamdeck"
)

const calendarWidgetFrameRate = 1

type CalendarWidgetOptions struct {
	Key      streamdeck.KeyID
	Size     int
	Location *time.Location
	Now      func() time.Time
	OpenApp  func(context.Context) error
}

type CalendarWidget struct {
	key     streamdeck.KeyID
	source  *calendarSource
	openApp func(context.Context) error
}

type calendarSource struct {
	size     int
	location *time.Location
	now      func() time.Time
	faces    calendarFaces
}

type calendarFaces struct {
	header font.Face
	day    font.Face
}

func NewCalendarWidget(options CalendarWidgetOptions) (*CalendarWidget, error) {
	if options.Size <= 0 {
		options.Size = DefaultClockWidgetSize
	}
	if options.Location == nil {
		options.Location = time.Local
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.OpenApp == nil {
		options.OpenApp = openCalendarApp
	}

	faces, err := loadCalendarFaces(options.Size)
	if err != nil {
		return nil, err
	}

	source := &calendarSource{
		size:     options.Size,
		location: options.Location,
		now:      options.Now,
		faces:    faces,
	}

	return &CalendarWidget{
		key:     options.Key,
		source:  source,
		openApp: options.OpenApp,
	}, nil
}

func (w *CalendarWidget) Button() deckbutton.Button {
	return deckbutton.Button{
		Key: w.key,
		Animation: &deckbutton.Animation{
			Source:    w.source,
			FrameRate: calendarWidgetFrameRate,
			Loop:      true,
		},
		OnPress: func(_ *streamdeck.Device, _ *streamdeck.Key) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return w.openApp(ctx)
		},
	}
}

func openCalendarApp(ctx context.Context) error {
	return exec.CommandContext(ctx, "open", "-a", "Calendar").Run()
}

func (s *calendarSource) Start(context.Context) error {
	return nil
}

func (s *calendarSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	now := s.now().In(s.location)
	img := image.NewRGBA(image.Rect(0, 0, s.size, s.size))
	s.render(img, now)
	return img, nil
}

func (s *calendarSource) Duration() time.Duration {
	return 0
}

func (s *calendarSource) Close() error {
	return closeFaces(s.faces.header, s.faces.day)
}

func (s *calendarSource) render(dst *image.RGBA, now time.Time) {
	fillVerticalGradient(dst, color.RGBA{R: 20, G: 20, B: 21, A: 255}, color.RGBA{R: 14, G: 14, B: 15, A: 255})

	header := stringsUpper(now.Format("Jan Mon"))
	day := now.Format("2")

	drawCenteredText(dst, s.faces.header, header, float64(s.size)/2, float64(scaledValue(s.size, 15)), color.RGBA{R: 205, G: 223, B: 81, A: 255})

	lineY := scaledValue(s.size, 21)
	for x := range s.size {
		dst.SetRGBA(x, lineY, color.RGBA{R: 86, G: 86, B: 90, A: 255})
	}

	lowerSectionCenterY := (float64(lineY) + float64(s.size)) / 2
	dayBaselineY := centeredTextBaselineY(s.faces.day, lowerSectionCenterY)
	drawCenteredText(dst, s.faces.day, day, float64(s.size)/2, dayBaselineY, color.RGBA{R: 248, G: 248, B: 248, A: 255})
}

func centeredTextBaselineY(face font.Face, centerY float64) float64 {
	metrics := face.Metrics()
	ascent := float64(metrics.Ascent.Round())
	descent := float64(metrics.Descent.Round())
	return centerY + ((ascent - descent) / 2)
}

func loadCalendarFaces(size int) (calendarFaces, error) {
	scale := float64(size) / 72.0
	header, err := newFace(gobold.TTF, 10*scale)
	if err != nil {
		return calendarFaces{}, fmt.Errorf("load calendar header font: %w", err)
	}
	day, err := newFace(gobold.TTF, 36*scale)
	if err != nil {
		return calendarFaces{}, fmt.Errorf("load calendar day font: %w", err)
	}

	return calendarFaces{
		header: header,
		day:    day,
	}, nil
}
