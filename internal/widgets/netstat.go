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
	psnet "github.com/shirou/gopsutil/v4/net"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"rafaelmartins.com/p/streamdeck"
)

const netstatWidgetFrameRate = 1

type NetstatCounterProvider func(ctx context.Context, iface string) (bytesRecv uint64, bytesSent uint64, err error)

type NetstatWidgetOptions struct {
	Key       streamdeck.KeyID
	Size      int
	Interface string
	Stats     NetstatCounterProvider
	Now       func() time.Time
	OpenApp   ActivityMonitorOpenFunc
}

type NetstatWidget struct {
	key     streamdeck.KeyID
	source  *netstatSource
	openApp ActivityMonitorOpenFunc
}

type netstatSource struct {
	size      int
	iface     string
	stats     NetstatCounterProvider
	now       func() time.Time
	faces     netstatFaces
	sampleMu  sync.Mutex
	lastTime  time.Time
	lastRecv  uint64
	lastSent  uint64
	hasSample bool
}

type netstatFaces struct {
	value font.Face
	iface font.Face
}

type bandwidthDirection int

const (
	bandwidthDirectionOut bandwidthDirection = iota
	bandwidthDirectionIn
)

func NewNetstatWidget(options NetstatWidgetOptions) (*NetstatWidget, error) {
	if options.Size <= 0 {
		options.Size = DefaultClockWidgetSize
	}
	if options.Interface == "" {
		return nil, fmt.Errorf("network interface is required")
	}
	if options.Stats == nil {
		options.Stats = defaultNetstatProvider
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.OpenApp == nil {
		options.OpenApp = openActivityMonitorNetwork
	}

	faces, err := loadNetstatFaces(options.Size)
	if err != nil {
		return nil, err
	}

	return &NetstatWidget{
		key:     options.Key,
		openApp: options.OpenApp,
		source: &netstatSource{
			size:  options.Size,
			iface: options.Interface,
			stats: options.Stats,
			now:   options.Now,
			faces: faces,
		},
	}, nil
}

func (w *NetstatWidget) Button() deckbutton.Button {
	return deckbutton.Button{
		Key: w.key,
		Animation: &deckbutton.Animation{
			Source:    w.source,
			FrameRate: netstatWidgetFrameRate,
			Loop:      true,
		},
		OnPress: func(_ *streamdeck.Device, _ *streamdeck.Key) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return w.openApp(ctx)
		},
	}
}

func defaultNetstatProvider(ctx context.Context, iface string) (uint64, uint64, error) {
	stats, err := psnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return 0, 0, fmt.Errorf("read network counters: %w", err)
	}

	for _, stat := range stats {
		if stat.Name == iface {
			return stat.BytesRecv, stat.BytesSent, nil
		}
	}

	return 0, 0, fmt.Errorf("network interface %q not found", iface)
}

func (s *netstatSource) Start(context.Context) error {
	return nil
}

func (s *netstatSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	inBytesPerSecond, outBytesPerSecond, err := s.readRates(ctx)
	if err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, s.size, s.size))
	s.render(img, inBytesPerSecond, outBytesPerSecond)
	return img, nil
}

func (s *netstatSource) Duration() time.Duration {
	return 0
}

func (s *netstatSource) Close() error {
	return closeFaces(s.faces.value, s.faces.iface)
}

func (s *netstatSource) readRates(ctx context.Context) (float64, float64, error) {
	recv, sent, err := s.stats(ctx, s.iface)
	if err != nil {
		return 0, 0, err
	}

	now := s.now()

	s.sampleMu.Lock()
	defer s.sampleMu.Unlock()

	if !s.hasSample {
		s.hasSample = true
		s.lastTime = now
		s.lastRecv = recv
		s.lastSent = sent
		return 0, 0, nil
	}

	elapsed := now.Sub(s.lastTime).Seconds()
	if elapsed <= 0 {
		s.lastTime = now
		s.lastRecv = recv
		s.lastSent = sent
		return 0, 0, nil
	}

	inDelta := counterDelta(recv, s.lastRecv)
	outDelta := counterDelta(sent, s.lastSent)

	s.lastTime = now
	s.lastRecv = recv
	s.lastSent = sent

	return float64(inDelta) / elapsed, float64(outDelta) / elapsed, nil
}

func counterDelta(current uint64, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}

func (s *netstatSource) render(dst *image.RGBA, inBytesPerSecond float64, outBytesPerSecond float64) {
	fillVerticalGradient(dst, color.RGBA{R: 11, G: 16, B: 24, A: 255}, color.RGBA{R: 7, G: 11, B: 18, A: 255})

	labelHeight := int(math.Round(float64(s.size) * 0.20))
	if labelHeight < 1 {
		labelHeight = 1
	}
	contentHeight := s.size - labelHeight
	rowHeight := contentHeight / 2
	firstDividerY := rowHeight
	secondDividerY := contentHeight

	for x := range s.size {
		dst.SetRGBA(x, firstDividerY, color.RGBA{R: 78, G: 86, B: 97, A: 255})
		dst.SetRGBA(x, secondDividerY, color.RGBA{R: 78, G: 86, B: 97, A: 255})
	}

	s.renderBandwidthRow(dst, float64(rowHeight)/2, bandwidthDirectionOut, formatBandwidth(outBytesPerSecond), color.RGBA{R: 255, G: 180, B: 97, A: 255})
	s.renderBandwidthRow(dst, float64(rowHeight)+(float64(rowHeight)/2), bandwidthDirectionIn, formatBandwidth(inBytesPerSecond), color.RGBA{R: 82, G: 224, B: 255, A: 255})

	labelCenterY := float64(contentHeight) + (float64(labelHeight) / 2)
	drawCenteredText(dst, s.faces.iface, s.iface, float64(s.size)/2, centeredTextBaselineY(s.faces.iface, labelCenterY), color.RGBA{R: 184, G: 191, B: 200, A: 255})
}

func (s *netstatSource) renderBandwidthRow(dst *image.RGBA, centerY float64, direction bandwidthDirection, value string, accent color.RGBA) {
	iconSize := float64(scaledValue(s.size, 8))
	valueWidth := measureTextWidth(s.faces.value, value)
	gap := float64(scaledValue(s.size, 3))
	totalWidth := iconSize + gap + valueWidth
	startX := (float64(s.size) - totalWidth) / 2

	iconCenterX := startX + (iconSize / 2)
	valueCenterX := startX + iconSize + gap + (valueWidth / 2)
	baselineY := centeredTextBaselineY(s.faces.value, centerY)

	drawBandwidthArrow(dst, iconCenterX, centerY, iconSize, direction, accent)
	drawCenteredText(dst, s.faces.value, value, valueCenterX, baselineY, color.RGBA{R: 245, G: 247, B: 250, A: 255})
}

func loadNetstatFaces(size int) (netstatFaces, error) {
	scale := float64(size) / 72.0
	value, err := newFace(gobold.TTF, 9.5*scale)
	if err != nil {
		return netstatFaces{}, fmt.Errorf("load netstat value font: %w", err)
	}
	iface, err := newFace(gomono.TTF, 10.5*scale)
	if err != nil {
		return netstatFaces{}, fmt.Errorf("load netstat interface font: %w", err)
	}

	return netstatFaces{
		value: value,
		iface: iface,
	}, nil
}

func formatBandwidth(bytesPerSecond float64) string {
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}

	type unit struct {
		suffix string
		value  float64
	}

	units := []unit{
		{suffix: "G/s", value: 1000 * 1000 * 1000},
		{suffix: "M/s", value: 1000 * 1000},
		{suffix: "K/s", value: 1000},
	}
	for _, unit := range units {
		if bytesPerSecond >= unit.value {
			return fmt.Sprintf("%.1f%s", bytesPerSecond/unit.value, unit.suffix)
		}
	}

	return fmt.Sprintf("%.0fB/s", bytesPerSecond)
}

func drawBandwidthArrow(dst *image.RGBA, centerX, centerY, size float64, direction bandwidthDirection, c color.RGBA) {
	shaftWidth := float64(scaledValue(dst.Bounds().Dx(), 2))
	headWidth := size * 0.95
	headHeight := size * 0.45
	shaftHalf := shaftWidth / 2
	shaftTop := centerY - (size / 2) + headHeight
	shaftBottom := centerY + (size / 2)
	headBaseY := centerY - (size / 2) + headHeight
	headTipY := centerY - (size / 2)

	if direction == bandwidthDirectionIn {
		shaftTop = centerY - (size / 2)
		shaftBottom = centerY + (size / 2) - headHeight
		headBaseY = centerY + (size / 2) - headHeight
		headTipY = centerY + (size / 2)
	}

	fillRect(
		dst,
		int(math.Round(centerX-shaftHalf)),
		int(math.Round(shaftTop)),
		int(math.Round(centerX+shaftHalf)),
		int(math.Round(shaftBottom)),
		c,
	)

	head := []polygonPoint{
		{x: centerX - (headWidth / 2), y: headBaseY},
		{x: centerX, y: headTipY},
		{x: centerX + (headWidth / 2), y: headBaseY},
	}
	if direction == bandwidthDirectionIn {
		head = []polygonPoint{
			{x: centerX - (headWidth / 2), y: headBaseY},
			{x: centerX + (headWidth / 2), y: headBaseY},
			{x: centerX, y: headTipY},
		}
	}
	drawPolygonFill(dst, head, c)
}
