package widgets

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"rafaelmartins.com/p/streamdeck"
)

const (
	DefaultClockWidgetSize = 144
	clockWidgetFrameRate   = 1
)

type ClockMode int

const (
	ClockModeAnalog ClockMode = iota
	ClockModeDigital
)

func (m ClockMode) String() string {
	switch m {
	case ClockModeAnalog:
		return "analog"
	case ClockModeDigital:
		return "digital"
	default:
		return "unknown"
	}
}

type ClockWidgetOptions struct {
	Key         streamdeck.KeyID
	Size        int
	Location    *time.Location
	Now         func() time.Time
	InitialMode ClockMode
}

type ClockWidget struct {
	key    streamdeck.KeyID
	source *clockSource
}

func NewClockWidget(options ClockWidgetOptions) (*ClockWidget, error) {
	if options.Size <= 0 {
		options.Size = DefaultClockWidgetSize
	}
	if options.Location == nil {
		options.Location = time.Local
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	source, err := newClockSource(options.Size, options.Location, options.Now, options.InitialMode)
	if err != nil {
		return nil, err
	}

	return &ClockWidget{
		key:    options.Key,
		source: source,
	}, nil
}

func (w *ClockWidget) Button() deckbutton.Button {
	return deckbutton.Button{
		Key: w.key,
		Animation: &deckbutton.Animation{
			Source:    w.source,
			FrameRate: clockWidgetFrameRate,
			Loop:      true,
		},
		OnPress: func(d *streamdeck.Device, k *streamdeck.Key) error {
			w.Toggle()
			img, err := w.source.FrameAt(context.Background(), 0)
			if err != nil {
				return err
			}
			return d.SetKeyImage(k.GetID(), img)
		},
	}
}

func (w *ClockWidget) Toggle() {
	w.source.ToggleMode()
}

func (w *ClockWidget) Mode() ClockMode {
	return w.source.Mode()
}

type clockSource struct {
	size     int
	location *time.Location
	now      func() time.Time
	faces    clockFaces

	mu       sync.RWMutex
	renderMu sync.Mutex
	mode     ClockMode
}

type clockFaces struct {
	main  font.Face
	small font.Face
	tiny  font.Face
}

func newClockSource(size int, location *time.Location, now func() time.Time, initialMode ClockMode) (*clockSource, error) {
	faces, err := loadClockFaces(size)
	if err != nil {
		return nil, err
	}

	if initialMode != ClockModeDigital {
		initialMode = ClockModeAnalog
	}

	return &clockSource{
		size:     size,
		location: location,
		now:      now,
		faces:    faces,
		mode:     initialMode,
	}, nil
}

func (s *clockSource) Start(context.Context) error {
	return nil
}

func (s *clockSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.renderMu.Lock()
	defer s.renderMu.Unlock()

	now := s.now().In(s.location)
	mode := s.Mode()
	img := image.NewRGBA(image.Rect(0, 0, s.size, s.size))

	if mode == ClockModeAnalog {
		s.renderAnalog(img, now)
		return img, nil
	}

	s.renderDigital(img, now)
	return img, nil
}

func (s *clockSource) Duration() time.Duration {
	return 0
}

func (s *clockSource) Close() error {
	return closeFaces(s.faces.main, s.faces.small, s.faces.tiny)
}

func (s *clockSource) ToggleMode() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode == ClockModeAnalog {
		s.mode = ClockModeDigital
		return
	}
	s.mode = ClockModeAnalog
}

func (s *clockSource) Mode() ClockMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

func loadClockFaces(size int) (clockFaces, error) {
	scale := float64(size) / 72.0

	main, err := newFace(gobold.TTF, 20*scale)
	if err != nil {
		return clockFaces{}, fmt.Errorf("load main clock font: %w", err)
	}
	small, err := newFace(gomono.TTF, 11*scale)
	if err != nil {
		return clockFaces{}, fmt.Errorf("load small clock font: %w", err)
	}
	tiny, err := newFace(gomono.TTF, 9*scale)
	if err != nil {
		return clockFaces{}, fmt.Errorf("load tiny clock font: %w", err)
	}

	return clockFaces{
		main:  main,
		small: small,
		tiny:  tiny,
	}, nil
}

func newFace(ttf []byte, size float64) (font.Face, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}

	return opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

func closeFaces(faces ...font.Face) error {
	for _, face := range faces {
		if face == nil {
			continue
		}
		if err := face.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *clockSource) renderAnalog(dst *image.RGBA, now time.Time) {
	fillSolid(dst, color.RGBA{R: 9, G: 9, B: 10, A: 255})

	center := float64(s.size) / 2
	radius := float64(s.size) * 0.40
	tickColor := color.RGBA{R: 0, G: 238, B: 255, A: 255}

	for i := range 12 {
		angle := (float64(i)/12)*2*math.Pi - math.Pi/2
		outer := radius
		inner := radius - float64(scaledValue(s.size, 6))
		width := float64(scaledValue(s.size, 2))
		if i%3 == 0 {
			inner = radius - float64(scaledValue(s.size, 8))
			width = float64(scaledValue(s.size, 3))
		}
		drawLineWidth(
			dst,
			center+math.Cos(angle)*inner,
			center+math.Sin(angle)*inner,
			center+math.Cos(angle)*outer,
			center+math.Sin(angle)*outer,
			width,
			tickColor,
		)
	}

	second := float64(now.Second()) + float64(now.Nanosecond())/float64(time.Second)
	minute := float64(now.Minute()) + second/60
	hour := float64(now.Hour()%12) + minute/60

	drawHandWithTail(
		dst,
		center,
		center,
		-float64(scaledValue(s.size, 2)),
		radius*0.34,
		(hour/12)*2*math.Pi-math.Pi/2,
		float64(scaledValue(s.size, 5)),
		color.RGBA{R: 255, G: 166, B: 0, A: 255},
	)
	drawHandWithTail(
		dst,
		center,
		center,
		-float64(scaledValue(s.size, 2)),
		radius*0.54,
		(minute/60)*2*math.Pi-math.Pi/2,
		float64(scaledValue(s.size, 4)),
		color.RGBA{R: 255, G: 214, B: 10, A: 255},
	)
	drawHandWithTail(
		dst,
		center,
		center,
		-radius*0.04,
		radius*0.74,
		(second/60)*2*math.Pi-math.Pi/2,
		float64(scaledValue(s.size, 1)),
		color.RGBA{R: 146, G: 151, B: 160, A: 255},
	)

	drawFilledCircle(dst, center, center, float64(scaledValue(s.size, 4)), color.RGBA{R: 255, G: 166, B: 0, A: 255})
	drawFilledCircle(dst, center, center, float64(scaledValue(s.size, 2)), color.RGBA{R: 245, G: 245, B: 245, A: 255})
}

func (s *clockSource) renderDigital(dst *image.RGBA, now time.Time) {
	digitWidth := float64(scaledValue(s.size, 13))
	digitHeight := float64(scaledValue(s.size, 26))
	thickness := float64(scaledValue(s.size, 3))
	digitGap := float64(scaledValue(s.size, 1))
	colonGap := float64(scaledValue(s.size, 2))
	colonWidth := float64(scaledValue(s.size, 3))

	totalWidth := (digitWidth * 4) + (digitGap * 2) + (colonGap * 2) + colonWidth
	startX := (float64(s.size) - totalWidth) / 2
	startY := (float64(s.size) - digitHeight) / 2

	segmentColor := color.RGBA{R: 197, G: 148, B: 255, A: 255}

	digits := now.Format("1504")
	x := startX
	for idx, r := range digits {
		drawSevenSegmentDigit(dst, x, startY, digitWidth, digitHeight, thickness, r, segmentColor)
		x += digitWidth
		if idx == 1 {
			x += colonGap
			if now.Second()%2 == 0 {
				drawColon(dst, x+(colonWidth/2), startY+(digitHeight/2), float64(scaledValue(s.size, 2)), segmentColor)
			}
			x += colonWidth + colonGap
			continue
		}
		if idx < len(digits)-1 {
			x += digitGap
		}
	}
}

func fillVerticalGradient(dst *image.RGBA, top color.RGBA, bottom color.RGBA) {
	bounds := dst.Bounds()
	height := bounds.Dy()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		t := float64(y-bounds.Min.Y) / float64(height-1)
		row := mixColor(top, bottom, t)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dst.SetRGBA(x, y, row)
		}
	}
}

func fillSolid(dst *image.RGBA, c color.RGBA) {
	bounds := dst.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dst.SetRGBA(x, y, c)
		}
	}
}

func fillRoundedRectVerticalGradient(dst *image.RGBA, x0, y0, x1, y1, radius float64, top color.RGBA, bottom color.RGBA) {
	bounds := dst.Bounds()
	height := y1 - y0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		yy := float64(y) + 0.5
		if yy < y0 || yy > y1 {
			continue
		}
		t := (yy - y0) / height
		row := mixColor(top, bottom, t)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			xx := float64(x) + 0.5
			if pointInRoundedRect(xx, yy, x0, y0, x1, y1, radius) {
				dst.SetRGBA(x, y, row)
			}
		}
	}
}

func strokeRoundedRect(dst *image.RGBA, x0, y0, x1, y1, radius, width float64, c color.RGBA) {
	innerRadius := radius - width
	if innerRadius < 0 {
		innerRadius = 0
	}
	bounds := dst.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		yy := float64(y) + 0.5
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			xx := float64(x) + 0.5
			outer := pointInRoundedRect(xx, yy, x0, y0, x1, y1, radius)
			inner := pointInRoundedRect(xx, yy, x0+width, y0+width, x1-width, y1-width, innerRadius)
			if outer && !inner {
				dst.SetRGBA(x, y, c)
			}
		}
	}
}

func pointInRoundedRect(x, y, x0, y0, x1, y1, radius float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}

	if x >= x0+radius && x <= x1-radius {
		return true
	}
	if y >= y0+radius && y <= y1-radius {
		return true
	}

	centers := [4][2]float64{
		{x0 + radius, y0 + radius},
		{x1 - radius, y0 + radius},
		{x0 + radius, y1 - radius},
		{x1 - radius, y1 - radius},
	}
	r2 := radius * radius
	for _, center := range centers {
		dx := x - center[0]
		dy := y - center[1]
		if dx*dx+dy*dy <= r2 {
			return true
		}
	}

	return false
}

func drawFilledCircle(dst *image.RGBA, cx, cy, radius float64, c color.RGBA) {
	bounds := dst.Bounds()
	r2 := radius * radius
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			if dx*dx+dy*dy <= r2 {
				dst.SetRGBA(x, y, c)
			}
		}
	}
}

func drawRing(dst *image.RGBA, cx, cy, outerRadius, innerRadius float64, c color.RGBA) {
	bounds := dst.Bounds()
	outer2 := outerRadius * outerRadius
	inner2 := innerRadius * innerRadius
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d2 := dx*dx + dy*dy
			if d2 <= outer2 && d2 >= inner2 {
				dst.SetRGBA(x, y, c)
			}
		}
	}
}

func drawHand(dst *image.RGBA, cx, cy, length, angle, width float64, c color.RGBA) {
	x2 := cx + math.Cos(angle)*length
	y2 := cy + math.Sin(angle)*length
	drawLineWidth(dst, cx, cy, x2, y2, width, c)
}

func drawHandWithTail(dst *image.RGBA, cx, cy, tail, length, angle, width float64, c color.RGBA) {
	x1 := cx + math.Cos(angle)*tail
	y1 := cy + math.Sin(angle)*tail
	x2 := cx + math.Cos(angle)*length
	y2 := cy + math.Sin(angle)*length
	drawLineWidth(dst, x1, y1, x2, y2, width, c)
}

func drawLineWidth(dst *image.RGBA, x1, y1, x2, y2, width float64, c color.RGBA) {
	bounds := dst.Bounds()
	radius := width / 2
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if distanceToSegment(float64(x)+0.5, float64(y)+0.5, x1, y1, x2, y2) <= radius {
				dst.SetRGBA(x, y, c)
			}
		}
	}
}

func distanceToSegment(px, py, x1, y1, x2, y2 float64) float64 {
	dx := x2 - x1
	dy := y2 - y1
	if dx == 0 && dy == 0 {
		return math.Hypot(px-x1, py-y1)
	}

	t := ((px-x1)*dx + (py-y1)*dy) / (dx*dx + dy*dy)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}

	projX := x1 + t*dx
	projY := y1 + t*dy
	return math.Hypot(px-projX, py-projY)
}

func drawCenteredText(dst draw.Image, face font.Face, text string, centerX, baselineY float64, c color.RGBA) {
	drawer := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: face,
	}

	advance := drawer.MeasureString(text)
	x := fixed.I(int(math.Round(centerX))) - advance/2
	y := fixed.I(int(math.Round(baselineY)))
	drawer.Dot = fixed.Point26_6{X: x, Y: y}
	drawer.DrawString(text)
}

func drawInsetFrame(dst *image.RGBA, inset int, c color.RGBA) {
	bounds := dst.Bounds()
	for x := bounds.Min.X + inset; x < bounds.Max.X-inset; x++ {
		dst.SetRGBA(x, bounds.Min.Y+inset, c)
		dst.SetRGBA(x, bounds.Max.Y-inset-1, c)
	}
	for y := bounds.Min.Y + inset; y < bounds.Max.Y-inset; y++ {
		dst.SetRGBA(bounds.Min.X+inset, y, c)
		dst.SetRGBA(bounds.Max.X-inset-1, y, c)
	}
}

func drawProgressBar(dst *image.RGBA, x, y, width, height int, progress float64, bg color.RGBA, fg color.RGBA) {
	for yy := y; yy < y+height; yy++ {
		for xx := x; xx < x+width; xx++ {
			dst.SetRGBA(xx, yy, bg)
		}
	}

	fillWidth := int(math.Round(progress * float64(width)))
	if fillWidth < 0 {
		fillWidth = 0
	}
	if fillWidth > width {
		fillWidth = width
	}

	for yy := y; yy < y+height; yy++ {
		for xx := x; xx < x+fillWidth; xx++ {
			dst.SetRGBA(xx, yy, fg)
		}
	}
}

type polygonPoint struct {
	x float64
	y float64
}

func drawSevenSegmentDigit(dst *image.RGBA, x, y, width, height, thickness float64, digit rune, fill color.RGBA) {
	segments := sevenSegmentPolygons(x, y, width, height, thickness)
	active := digitSegments(digit)
	for _, key := range active {
		drawPolygonFill(dst, segments[key], fill)
	}
}

func digitSegments(digit rune) []byte {
	switch digit {
	case '0':
		return []byte{'a', 'b', 'c', 'd', 'e', 'f'}
	case '1':
		return []byte{'b', 'c'}
	case '2':
		return []byte{'a', 'b', 'g', 'e', 'd'}
	case '3':
		return []byte{'a', 'b', 'c', 'd', 'g'}
	case '4':
		return []byte{'f', 'g', 'b', 'c'}
	case '5':
		return []byte{'a', 'f', 'g', 'c', 'd'}
	case '6':
		return []byte{'a', 'f', 'e', 'd', 'c', 'g'}
	case '7':
		return []byte{'a', 'b', 'c'}
	case '8':
		return []byte{'a', 'b', 'c', 'd', 'e', 'f', 'g'}
	case '9':
		return []byte{'a', 'b', 'c', 'd', 'f', 'g'}
	default:
		return nil
	}
}

func sevenSegmentPolygons(x, y, width, height, thickness float64) map[byte][]polygonPoint {
	slant := math.Max(1, thickness*0.45)
	hLen := width - thickness
	vLen := (height - 3*thickness) / 2

	return map[byte][]polygonPoint{
		'a': horizontalSegment(x+(thickness/2), y, hLen, thickness, slant),
		'g': horizontalSegment(x+(thickness/2), y+((height-thickness)/2), hLen, thickness, slant),
		'd': horizontalSegment(x+(thickness/2), y+height-thickness, hLen, thickness, slant),
		'f': verticalSegment(x, y+(thickness/2), thickness, vLen, slant),
		'b': verticalSegment(x+width-thickness, y+(thickness/2), thickness, vLen, slant),
		'e': verticalSegment(x, y+(height/2), thickness, vLen, slant),
		'c': verticalSegment(x+width-thickness, y+(height/2), thickness, vLen, slant),
	}
}

func horizontalSegment(x, y, width, thickness, slant float64) []polygonPoint {
	return []polygonPoint{
		{x + slant, y},
		{x + width - slant, y},
		{x + width, y + (thickness / 2)},
		{x + width - slant, y + thickness},
		{x + slant, y + thickness},
		{x, y + (thickness / 2)},
	}
}

func verticalSegment(x, y, thickness, height, slant float64) []polygonPoint {
	return []polygonPoint{
		{x + slant, y},
		{x + thickness - slant, y},
		{x + thickness, y + slant},
		{x + thickness, y + height - slant},
		{x + thickness - slant, y + height},
		{x + slant, y + height},
		{x, y + height - slant},
		{x, y + slant},
	}
}

func drawColon(dst *image.RGBA, cx, cy, radius float64, fill color.RGBA) {
	offset := radius * 2.4
	drawFilledCircle(dst, cx, cy-offset, radius, fill)
	drawFilledCircle(dst, cx, cy+offset, radius, fill)
}

func drawPolygonFill(dst *image.RGBA, pts []polygonPoint, c color.RGBA) {
	if len(pts) == 0 {
		return
	}

	minX, maxX := pts[0].x, pts[0].x
	minY, maxY := pts[0].y, pts[0].y
	for _, pt := range pts[1:] {
		minX = math.Min(minX, pt.x)
		maxX = math.Max(maxX, pt.x)
		minY = math.Min(minY, pt.y)
		maxY = math.Max(maxY, pt.y)
	}

	bounds := dst.Bounds()
	startX := maxInt(bounds.Min.X, int(math.Floor(minX)))
	endX := minInt(bounds.Max.X-1, int(math.Ceil(maxX)))
	startY := maxInt(bounds.Min.Y, int(math.Floor(minY)))
	endY := minInt(bounds.Max.Y-1, int(math.Ceil(maxY)))

	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			if pointInPolygon(float64(x)+0.5, float64(y)+0.5, pts) {
				dst.SetRGBA(x, y, c)
			}
		}
	}
}

func pointInPolygon(x, y float64, pts []polygonPoint) bool {
	inside := false
	j := len(pts) - 1
	for i := range len(pts) {
		xi, yi := pts[i].x, pts[i].y
		xj, yj := pts[j].x, pts[j].y
		intersects := ((yi > y) != (yj > y)) && (x < (xj-xi)*(y-yi)/(yj-yi)+xi)
		if intersects {
			inside = !inside
		}
		j = i
	}
	return inside
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func mixColor(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: uint8(float64(a.A) + (float64(b.A)-float64(a.A))*t),
	}
}

func stringsUpper(s string) string {
	out := make([]byte, len(s))
	for i := range len(s) {
		b := s[i]
		if b >= 'a' && b <= 'z' {
			out[i] = b - 32
			continue
		}
		out[i] = b
	}
	return string(out)
}

func scaledValue(size int, base int) int {
	scaled := int(math.Round((float64(size) / 72.0) * float64(base)))
	if scaled < 1 {
		return 1
	}
	return scaled
}
