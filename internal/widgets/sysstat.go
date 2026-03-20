package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"github.com/shirou/gopsutil/v4/cpu"
	psmem "github.com/shirou/gopsutil/v4/mem"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"rafaelmartins.com/p/streamdeck"
)

const sysstatWidgetFrameRate = 1

type SysstatProvider func(ctx context.Context) (cpuPercent float64, memoryPercent float64, err error)

type SysstatWidgetOptions struct {
	Key   streamdeck.KeyID
	Size  int
	Stats SysstatProvider
}

type SysstatWidget struct {
	key    streamdeck.KeyID
	source *sysstatSource
}

type sysstatSource struct {
	size  int
	stats SysstatProvider
	faces sysstatFaces
}

type sysstatFaces struct {
	value font.Face
}

var (
	sysstatFacesMu    sync.Mutex
	sysstatFacesCache = map[int]sysstatFaces{}
)

func NewSysstatWidget(options SysstatWidgetOptions) (*SysstatWidget, error) {
	if options.Size <= 0 {
		options.Size = DefaultClockWidgetSize
	}
	if options.Stats == nil {
		options.Stats = defaultSysstatProvider
	}

	faces, err := loadSysstatFaces(options.Size)
	if err != nil {
		return nil, err
	}

	return &SysstatWidget{
		key: options.Key,
		source: &sysstatSource{
			size:  options.Size,
			stats: options.Stats,
			faces: faces,
		},
	}, nil
}

func (w *SysstatWidget) Button() deckbutton.Button {
	return deckbutton.Button{
		Key: w.key,
		Animation: &deckbutton.Animation{
			Source:    w.source,
			FrameRate: sysstatWidgetFrameRate,
			Loop:      true,
		},
	}
}

func defaultSysstatProvider(ctx context.Context) (float64, float64, error) {
	cpuPercents, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return 0, 0, fmt.Errorf("read cpu usage: %w", err)
	}
	if len(cpuPercents) == 0 {
		return 0, 0, fmt.Errorf("read cpu usage: empty result")
	}

	memStats, err := psmem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("read memory usage: %w", err)
	}

	return cpuPercents[0], memStats.UsedPercent, nil
}

func (s *sysstatSource) Start(context.Context) error {
	return nil
}

func (s *sysstatSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	cpuPercent, memoryPercent, err := s.stats(ctx)
	if err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, s.size, s.size))
	s.render(img, clampUsage(cpuPercent), clampUsage(memoryPercent))
	return img, nil
}

func (s *sysstatSource) Duration() time.Duration {
	return 0
}

func (s *sysstatSource) Close() error {
	return nil
}

func (s *sysstatSource) render(dst *image.RGBA, cpuPercent float64, memoryPercent float64) {
	fillVerticalGradient(dst, color.RGBA{R: 12, G: 14, B: 18, A: 255}, color.RGBA{R: 8, G: 9, B: 12, A: 255})

	dividerY := s.size / 2
	for x := range s.size {
		dst.SetRGBA(x, dividerY, color.RGBA{R: 84, G: 88, B: 94, A: 255})
	}

	s.renderUsageRow(dst, float64(dividerY)/2, cpuPercent, color.RGBA{R: 255, G: 169, B: 77, A: 255}, drawCPUIcon)
	s.renderUsageRow(dst, (float64(dividerY)+float64(s.size))/2, memoryPercent, color.RGBA{R: 94, G: 220, B: 222, A: 255}, drawMemoryIcon)
}

func (s *sysstatSource) renderUsageRow(dst *image.RGBA, centerY float64, percent float64, iconColor color.RGBA, drawIcon func(*image.RGBA, int, int, int, color.RGBA)) {
	valueText := fmt.Sprintf("%d%%", int(math.Round(percent)))
	iconSize := float64(scaledValue(s.size, 11))
	gap := float64(scaledValue(s.size, 4))
	textWidth := measureTextWidth(s.faces.value, valueText)
	totalWidth := iconSize + gap + textWidth
	startX := (float64(s.size) - totalWidth) / 2

	iconX := int(math.Round(startX))
	iconY := int(math.Round(centerY - (iconSize / 2)))
	drawIcon(dst, iconX, iconY, int(math.Round(iconSize)), iconColor)

	textCenterX := startX + iconSize + gap + (textWidth / 2)
	drawCenteredText(dst, s.faces.value, valueText, textCenterX, centeredTextBaselineY(s.faces.value, centerY), color.RGBA{R: 246, G: 247, B: 248, A: 255})
}

func loadSysstatFaces(size int) (sysstatFaces, error) {
	sysstatFacesMu.Lock()
	defer sysstatFacesMu.Unlock()

	if faces, ok := sysstatFacesCache[size]; ok {
		return faces, nil
	}

	scale := float64(size) / 72.0
	value, err := newFace(gobold.TTF, 15*scale)
	if err != nil {
		return sysstatFaces{}, fmt.Errorf("load sysstat value font: %w", err)
	}

	faces := sysstatFaces{value: value}
	sysstatFacesCache[size] = faces
	return faces, nil
}

func clampUsage(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func measureTextWidth(face font.Face, text string) float64 {
	return float64((&font.Drawer{Face: face}).MeasureString(text).Round())
}

func drawCPUIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	thickness := maxInt(1, size/10)
	pinLength := maxInt(2, size/6)
	outerInset := maxInt(2, size/4)
	innerInset := outerInset + maxInt(2, size/7)
	pinOffsets := []int{size / 3, size / 2, (size * 2) / 3}

	strokeRect(dst, x+outerInset, y+outerInset, x+size-outerInset, y+size-outerInset, thickness, c)
	strokeRect(dst, x+innerInset, y+innerInset, x+size-innerInset, y+size-innerInset, thickness, c)

	for _, offset := range pinOffsets {
		fillRect(dst, x+offset-(thickness/2), y, x+offset-(thickness/2)+thickness, y+pinLength, c)
		fillRect(dst, x+offset-(thickness/2), y+size-pinLength, x+offset-(thickness/2)+thickness, y+size, c)
		fillRect(dst, x, y+offset-(thickness/2), x+pinLength, y+offset-(thickness/2)+thickness, c)
		fillRect(dst, x+size-pinLength, y+offset-(thickness/2), x+size, y+offset-(thickness/2)+thickness, c)
	}
}

func drawMemoryIcon(dst *image.RGBA, x, y, size int, c color.RGBA) {
	thickness := maxInt(1, size/10)
	boardX0 := x + maxInt(2, size/8)
	boardX1 := x + size - maxInt(2, size/8)
	boardY0 := y + maxInt(4, size/3)
	boardY1 := y + size - maxInt(3, size/5)
	slotTop := boardY0 + thickness + 1
	slotBottom := boardY1 - thickness - 1
	slotWidth := maxInt(2, (boardX1-boardX0)/7)

	strokeRect(dst, boardX0, boardY0, boardX1, boardY1, thickness, c)

	for i := range 3 {
		slotX0 := boardX0 + ((i + 1) * (boardX1 - boardX0) / 4) - (slotWidth / 2)
		fillRect(dst, slotX0, slotTop, slotX0+slotWidth, slotBottom, c)
	}

	contactWidth := maxInt(2, (boardX1-boardX0)/7)
	contactHeight := maxInt(2, size/8)
	for i := range 4 {
		contactX0 := boardX0 + maxInt(1, thickness) + (i * (boardX1 - boardX0 - contactWidth) / 3)
		fillRect(dst, contactX0, boardY1, contactX0+contactWidth, boardY1+contactHeight, c)
	}
}

func fillRect(dst *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	bounds := dst.Bounds()
	x0 = maxInt(bounds.Min.X, x0)
	y0 = maxInt(bounds.Min.Y, y0)
	x1 = minInt(bounds.Max.X, x1)
	y1 = minInt(bounds.Max.Y, y1)

	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			dst.SetRGBA(x, y, c)
		}
	}
}

func strokeRect(dst *image.RGBA, x0, y0, x1, y1, thickness int, c color.RGBA) {
	if x1 <= x0 || y1 <= y0 {
		return
	}

	fillRect(dst, x0, y0, x1, y0+thickness, c)
	fillRect(dst, x0, y1-thickness, x1, y1, c)
	fillRect(dst, x0, y0, x0+thickness, y1, c)
	fillRect(dst, x1-thickness, y0, x1, y1, c)
}
