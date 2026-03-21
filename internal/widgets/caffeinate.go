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
	enabledAt  time.Time
	updates    chan struct{}
}

type caffeinateFaces struct {
	title  font.Face
	timer  font.Face
	status font.Face
}

type commandCaffeinateBackend struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

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
			size:      options.Size,
			now:       options.Now,
			state:     options.Backend,
			faces:     faces,
			enabledAt: initialEnabledAt(options.Backend, options.Now),
			updates:   make(chan struct{}, 1),
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
		if err := w.backend.Disable(); err != nil {
			return err
		}
		w.source.markDisabled()
		return nil
	}
	if err := w.backend.Enable(); err != nil {
		return err
	}
	w.source.markEnabled(w.now())
	return nil
}

func (w *CaffeinateWidget) sleepNow() error {
	if w.backend.Enabled() {
		if err := w.backend.Disable(); err != nil {
			return err
		}
		w.source.markDisabled()
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

func (s *caffeinateSource) NextFrameDelay() time.Duration {
	s.mu.RLock()
	pressed := s.pressed
	s.mu.RUnlock()
	if pressed {
		return time.Second / caffeinateWidgetFrameRate
	}
	if s.state != nil && s.state.Enabled() {
		return time.Second
	}
	return 0
}

func (s *caffeinateSource) Updates() <-chan struct{} {
	return s.updates
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
	if err := closeFaces(s.faces.title, s.faces.timer, s.faces.status); err != nil {
		return err
	}
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
	s.notifyUpdate()
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
	s.notifyUpdate()
}

func (s *caffeinateSource) markEnabled(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabledAt = now
	s.notifyUpdate()
}

func (s *caffeinateSource) markDisabled() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabledAt = time.Time{}
	s.notifyUpdate()
}

func (s *caffeinateSource) enabledSince() (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.enabledAt.IsZero() {
		return time.Time{}, false
	}
	return s.enabledAt, true
}

func (s *caffeinateSource) notifyUpdate() {
	if s.updates == nil {
		return
	}
	select {
	case s.updates <- struct{}{}:
	default:
	}
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
	now := s.now()
	progress := s.holdProgress(now)
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
	rowGapOffset := float64(scaledValue(s.size, 5))
	headerCenterY := float64(s.size)*0.32 - rowGapOffset
	statusCenterY := float64(s.size)*0.53 + rowGapOffset
	headerColor := color.RGBA{R: 227, G: 231, B: 236, A: 255}
	status := "OFF"
	statusColor := color.RGBA{R: 171, G: 178, B: 186, A: 255}
	if isEnabled {
		headerColor = color.RGBA{R: 233, G: 237, B: 242, A: 255}
		if enabledAt, ok := s.enabledSince(); ok {
			drawCaffeinateElapsedTimer(dst, s.faces.timer, now.Sub(enabledAt), centerX, headerCenterY, headerColor)
		} else {
			drawCenteredText(dst, s.faces.title, "CAFFEINATE", centerX, centeredTextBaselineY(s.faces.title, headerCenterY), headerColor)
		}
		status = "ON"
		statusColor = color.RGBA{R: 105, G: 233, B: 137, A: 255}
	} else {
		drawCenteredText(dst, s.faces.title, "CAFFEINATE", centerX, centeredTextBaselineY(s.faces.title, headerCenterY), headerColor)
	}
	drawCenteredText(dst, s.faces.status, status, centerX, centeredTextBaselineY(s.faces.status, statusCenterY), statusColor)
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

func initialEnabledAt(backend CaffeinateBackend, now func() time.Time) time.Time {
	if backend == nil || !backend.Enabled() {
		return time.Time{}
	}
	return now()
}

func caffeinateElapsedParts(elapsed time.Duration) (string, string) {
	if elapsed < 0 {
		elapsed = 0
	}
	totalSeconds := int(elapsed / time.Second)
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%d", minutes), fmt.Sprintf("%02d", seconds)
}

func drawCaffeinateElapsedTimer(dst *image.RGBA, face font.Face, elapsed time.Duration, centerX, centerY float64, c color.RGBA) {
	minutes, seconds := caffeinateElapsedParts(elapsed)
	minutesWidth := measureTextWidth(face, minutes)
	colonWidth := measureTextWidth(face, ":")
	secondsWidth := measureTextWidth(face, seconds)
	startX := centerX - ((minutesWidth + colonWidth + secondsWidth) / 2)
	baselineY := centeredTextBaselineY(face, centerY)

	drawCenteredText(dst, face, minutes, startX+(minutesWidth/2), baselineY, c)
	drawCenteredText(dst, face, ":", startX+minutesWidth+(colonWidth/2), baselineY, c)
	drawCenteredText(dst, face, seconds, startX+minutesWidth+colonWidth+(secondsWidth/2), baselineY, c)
}

func drawCaffeinateCup(dst *image.RGBA, size int, enabled bool) {
	scale := float64(size) / 144.0
	iconScale := scale * 0.78
	stroke := color.RGBA{R: 245, G: 240, B: 232, A: 255}
	coffee := color.RGBA{R: 212, G: 162, B: 95, A: 255}
	if !enabled {
		stroke = color.RGBA{R: 164, G: 171, B: 179, A: 255}
		coffee = color.RGBA{R: 120, G: 126, B: 132, A: 255}
	}

	lineWidth := math.Max(2, 2.3*scale)
	cx := float64(size) * 0.45
	rimCY := float64(size) * 0.27
	rimRX := 29 * iconScale
	rimRY := 7.5 * iconScale
	bodyBottomY := float64(size) * 0.46
	bodyTopLeftX := cx - 24*iconScale
	bodyTopRightX := cx + 24*iconScale
	bodyBottomLeftX := cx - 18*iconScale
	bodyBottomRightX := cx + 18*iconScale

	drawEllipseArc(dst, cx, rimCY, rimRX, rimRY, 0, 2*math.Pi, lineWidth, stroke)
	drawLineWidth(dst, bodyTopLeftX, rimCY+1.5*iconScale, bodyBottomLeftX, bodyBottomY, lineWidth, stroke)
	drawLineWidth(dst, bodyTopRightX, rimCY+1.5*iconScale, bodyBottomRightX, bodyBottomY, lineWidth, stroke)
	drawEllipseArc(dst, cx, bodyBottomY, 18*iconScale, 5.5*iconScale, 0.05*math.Pi, 0.95*math.Pi, lineWidth, stroke)

	coffeeCY := rimCY + 2.2*iconScale
	drawEllipseArc(dst, cx, coffeeCY, 22*iconScale, 4.5*iconScale, 0.15*math.Pi, 0.85*math.Pi, lineWidth*0.9, coffee)
	for i := range 4 {
		offset := (-9.0 + float64(i)*6.0) * iconScale
		drawLineWidth(
			dst,
			cx+offset-4*iconScale, coffeeCY+1.8*iconScale,
			cx+offset+1.5*iconScale, coffeeCY-1.5*iconScale,
			math.Max(1.2, 1.4*scale),
			coffee,
		)
	}

	handleCX := cx + 31*iconScale
	handleCY := float64(size) * 0.38
	drawEllipseArc(dst, handleCX, handleCY, 12*iconScale, 15*iconScale, -0.45*math.Pi, 0.75*math.Pi, lineWidth, stroke)
	drawEllipseArc(dst, handleCX, handleCY, 6*iconScale, 9*iconScale, -0.3*math.Pi, 0.6*math.Pi, lineWidth, stroke)

	plateCY := float64(size) * 0.58
	drawEllipseArc(dst, cx, plateCY, 48*iconScale, 12*iconScale, 0, 2*math.Pi, lineWidth, stroke)
	drawEllipseArc(dst, cx, plateCY-1.8*iconScale, 28*iconScale, 6*iconScale, 0.1*math.Pi, 0.9*math.Pi, math.Max(1.3, 1.5*scale), stroke)
	drawLineWidth(dst, cx-9*iconScale, bodyBottomY+4*iconScale, cx+9*iconScale, bodyBottomY+4*iconScale, lineWidth, stroke)

	if enabled {
		drawSteamPlume(dst, cx-15*iconScale, rimCY-2*iconScale, 24*iconScale, math.Max(1.5, 1.7*scale), stroke)
		drawSteamPlume(dst, cx, rimCY-6*iconScale, 28*iconScale, math.Max(1.5, 1.7*scale), stroke)
		drawSteamPlume(dst, cx+15*iconScale, rimCY-2*iconScale, 24*iconScale, math.Max(1.5, 1.7*scale), stroke)
	}
}

func drawSteamPlume(dst *image.RGBA, x, yBottom, height, width float64, c color.RGBA) {
	step := height / 4
	sway := math.Max(2, height/7)
	drawPolylineWidth(dst, []polygonPoint{
		{x: x, y: yBottom},
		{x: x - sway, y: yBottom - step},
		{x: x + sway*0.45, y: yBottom - 2*step},
		{x: x - sway*0.35, y: yBottom - 3*step},
		{x: x + sway*0.8, y: yBottom - 4*step},
	}, width, c)
}

func drawPolylineWidth(dst *image.RGBA, points []polygonPoint, width float64, c color.RGBA) {
	if len(points) < 2 {
		return
	}
	for i := 1; i < len(points); i++ {
		drawLineWidth(dst, points[i-1].x, points[i-1].y, points[i].x, points[i].y, width, c)
	}
}

func drawEllipseArc(dst *image.RGBA, cx, cy, rx, ry, startAngle, endAngle, width float64, c color.RGBA) {
	if rx <= 0 || ry <= 0 {
		return
	}
	span := math.Abs(endAngle - startAngle)
	segments := int(math.Max(16, math.Ceil(span*math.Max(rx, ry)/4)))
	prevX := cx + math.Cos(startAngle)*rx
	prevY := cy + math.Sin(startAngle)*ry
	for i := 1; i <= segments; i++ {
		t := float64(i) / float64(segments)
		angle := startAngle + (endAngle-startAngle)*t
		x := cx + math.Cos(angle)*rx
		y := cy + math.Sin(angle)*ry
		drawLineWidth(dst, prevX, prevY, x, y, width, c)
		prevX = x
		prevY = y
	}
}

func loadCaffeinateFaces(size int) (caffeinateFaces, error) {
	scale := float64(size) / 72.0
	title, err := newFace(gobold.TTF, 7.5*scale)
	if err != nil {
		return caffeinateFaces{}, fmt.Errorf("load caffeinate title font: %w", err)
	}
	timer, err := newFace(gobold.TTF, 12.5*scale)
	if err != nil {
		return caffeinateFaces{}, fmt.Errorf("load caffeinate timer font: %w", err)
	}
	status, err := newFace(gobold.TTF, 18*scale)
	if err != nil {
		return caffeinateFaces{}, fmt.Errorf("load caffeinate status font: %w", err)
	}

	return caffeinateFaces{
		title:  title,
		timer:  timer,
		status: status,
	}, nil
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
