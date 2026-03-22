package widgets

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"rafaelmartins.com/p/streamdeck"
)

const (
	weatherCacheRefreshInterval = 10 * time.Minute
	weatherCacheRetryInterval   = 30 * time.Second
	weatherViewUpdateInterval   = 5 * time.Minute
)

type WeatherFetchFunc func(ctx context.Context, location string) (weatherSnapshot, error)

type WeatherWidgetOptions struct {
	Location   string
	Size       int
	Fetch      WeatherFetchFunc
	Now        func() time.Time
	HTTPClient *http.Client
}

type WeatherWidget struct {
	location      string
	size          int
	fetch         WeatherFetchFunc
	now           func() time.Time
	httpClient    *http.Client
	todayFaces    weatherFaces
	forecastFaces weatherFaces

	startOnce sync.Once
	renderMu  sync.Mutex

	stateMu sync.RWMutex
	cached  weatherSnapshot
	hasData bool

	subscribers map[chan struct{}]struct{}
}

type WeatherTodayWidget struct {
	key    streamdeck.KeyID
	source *weatherViewSource
}

type WeatherForecastWidget struct {
	key    streamdeck.KeyID
	source *weatherViewSource
}

type weatherViewSource struct {
	widget  *WeatherWidget
	view    weatherViewKind
	faces   weatherFaces
	updates chan struct{}
}

type weatherViewKind int

const (
	weatherViewToday weatherViewKind = iota
	weatherViewForecast
)

type weatherFaces struct {
	todayDetail    font.Face
	todayTemp      font.Face
	today          font.Face
	forecastMain   font.Face
	forecastDetail font.Face
}

type weatherSnapshot struct {
	Location string
	Current  weatherCurrent
	Days     []weatherDay
}

type weatherCurrent struct {
	TempC     string
	Condition string
	UVIndex   string
}

type weatherDay struct {
	Date      time.Time
	MinTempC  string
	MaxTempC  string
	Condition string
	IconURL   string
	Icon      image.Image
}

type wttrResponse struct {
	CurrentCondition []struct {
		TempC       string `json:"temp_C"`
		UVIndex     string `json:"uvIndex"`
		WeatherDesc []struct {
			Value string `json:"value"`
		} `json:"weatherDesc"`
	} `json:"current_condition"`
	Weather []struct {
		Date     string `json:"date"`
		MintempC string `json:"mintempC"`
		MaxtempC string `json:"maxtempC"`
		Hourly   []struct {
			Time        string `json:"time"`
			WeatherDesc []struct {
				Value string `json:"value"`
			} `json:"weatherDesc"`
			WeatherIconURL []struct {
				Value string `json:"value"`
			} `json:"weatherIconUrl"`
		} `json:"hourly"`
	} `json:"weather"`
}

func NewWeatherWidget(options WeatherWidgetOptions) (*WeatherWidget, error) {
	if options.Location == "" {
		return nil, fmt.Errorf("weather location is required")
	}
	if options.Size <= 0 {
		options.Size = DefaultClockWidgetSize
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}

	todayFaces, err := loadWeatherFaces(options.Size)
	if err != nil {
		return nil, err
	}
	forecastFaces, err := loadWeatherFaces(options.Size)
	if err != nil {
		return nil, err
	}

	widget := &WeatherWidget{
		location:      options.Location,
		size:          options.Size,
		now:           options.Now,
		httpClient:    options.HTTPClient,
		todayFaces:    todayFaces,
		forecastFaces: forecastFaces,
		subscribers:   make(map[chan struct{}]struct{}),
	}
	if options.Fetch != nil {
		widget.fetch = options.Fetch
	} else {
		widget.fetch = widget.fetchSnapshot
	}

	return widget, nil
}

func (w *WeatherWidget) Today(key streamdeck.KeyID) *WeatherTodayWidget {
	return &WeatherTodayWidget{
		key: key,
		source: &weatherViewSource{
			widget: w,
			view:   weatherViewToday,
			faces:  w.todayFaces,
		},
	}
}

func (w *WeatherWidget) Forecast(key streamdeck.KeyID) *WeatherForecastWidget {
	return &WeatherForecastWidget{
		key: key,
		source: &weatherViewSource{
			widget: w,
			view:   weatherViewForecast,
			faces:  w.forecastFaces,
		},
	}
}

func (w *WeatherTodayWidget) Button() deckbutton.Button {
	return deckbutton.Button{
		Key: w.key,
		Animation: &deckbutton.Animation{
			Source:         w.source,
			UpdateInterval: weatherViewUpdateInterval,
			Loop:           true,
		},
	}
}

func (w *WeatherForecastWidget) Button() deckbutton.Button {
	return deckbutton.Button{
		Key: w.key,
		Animation: &deckbutton.Animation{
			Source:         w.source,
			UpdateInterval: weatherViewUpdateInterval,
			Loop:           true,
		},
	}
}

func (s *weatherViewSource) Start(ctx context.Context) error {
	if s.updates == nil {
		s.updates = make(chan struct{}, 1)
		s.widget.registerUpdates(s.updates)
	}
	return s.widget.start(ctx)
}

func (s *weatherViewSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.widget.renderMu.Lock()
	defer s.widget.renderMu.Unlock()

	img := image.NewRGBA(image.Rect(0, 0, s.widget.size, s.widget.size))
	snapshot, ok := s.widget.currentSnapshot()
	if !ok {
		s.renderUnavailable(img)
		return img, nil
	}

	switch s.view {
	case weatherViewToday:
		s.renderToday(img, snapshot)
	default:
		s.renderForecast(img, snapshot)
	}

	return img, nil
}

func (s *weatherViewSource) Duration() time.Duration {
	return 0
}

func (s *weatherViewSource) Updates() <-chan struct{} {
	return s.updates
}

func (s *weatherViewSource) Close() error {
	s.widget.unregisterUpdates(s.updates)

	return closeFaces(
		s.faces.todayDetail,
		s.faces.todayTemp,
		s.faces.today,
		s.faces.forecastMain,
		s.faces.forecastDetail,
	)
}

func (w *WeatherWidget) start(ctx context.Context) error {
	w.startOnce.Do(func() {
		err := w.refresh(ctx)

		go func() {
			delay := weatherCacheRefreshInterval
			if err != nil {
				delay = weatherCacheRetryInterval
			}

			timer := time.NewTimer(delay)
			defer timer.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					if err := w.refresh(ctx); err != nil {
						delay = weatherCacheRetryInterval
					} else {
						delay = weatherCacheRefreshInterval
					}
					timer.Reset(delay)
				}
			}
		}()
	})

	return nil
}

func (w *WeatherWidget) refresh(ctx context.Context) error {
	snapshot, err := w.fetch(ctx, w.location)
	if err != nil {
		return err
	}

	w.stateMu.Lock()
	w.cached = snapshot
	w.hasData = true
	w.stateMu.Unlock()
	w.notifySubscribers()
	return nil
}

func (w *WeatherWidget) registerUpdates(ch chan struct{}) {
	if ch == nil {
		return
	}

	w.stateMu.Lock()
	w.subscribers[ch] = struct{}{}
	w.stateMu.Unlock()
}

func (w *WeatherWidget) unregisterUpdates(ch chan struct{}) {
	if ch == nil {
		return
	}

	w.stateMu.Lock()
	delete(w.subscribers, ch)
	w.stateMu.Unlock()
}

func (w *WeatherWidget) notifySubscribers() {
	w.stateMu.RLock()
	subscribers := make([]chan struct{}, 0, len(w.subscribers))
	for ch := range w.subscribers {
		subscribers = append(subscribers, ch)
	}
	w.stateMu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (w *WeatherWidget) currentSnapshot() (weatherSnapshot, bool) {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()

	if !w.hasData {
		return weatherSnapshot{}, false
	}
	return w.cached, true
}

func (w *WeatherWidget) fetchSnapshot(ctx context.Context, location string) (weatherSnapshot, error) {
	endpoint := fmt.Sprintf("https://wttr.in/%s?format=j1", url.PathEscape(location))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return weatherSnapshot{}, fmt.Errorf("create weather request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return weatherSnapshot{}, fmt.Errorf("fetch weather: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return weatherSnapshot{}, fmt.Errorf("fetch weather: unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return weatherSnapshot{}, fmt.Errorf("read weather response: %w", err)
	}

	snapshot, err := parseWeatherSnapshot(body, location)
	if err != nil {
		return weatherSnapshot{}, err
	}

	return w.attachForecastIcons(ctx, snapshot), nil
}

func (w *WeatherWidget) fetchWeatherIcon(ctx context.Context, iconURL string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iconURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create weather icon request: %w", err)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch weather icon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch weather icon: unexpected status %s", resp.Status)
	}

	icon, _, err := image.Decode(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("decode weather icon: %w", err)
	}
	return icon, nil
}

func parseWeatherSnapshot(body []byte, location string) (weatherSnapshot, error) {
	var payload wttrResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return weatherSnapshot{}, fmt.Errorf("decode weather response: %w", err)
	}

	snapshot := weatherSnapshot{
		Location: location,
	}

	if len(payload.CurrentCondition) > 0 {
		current := payload.CurrentCondition[0]
		snapshot.Current = weatherCurrent{
			TempC:     cleanTemperature(current.TempC),
			Condition: cleanWeatherText(firstWeatherText(current.WeatherDesc), 18),
			UVIndex:   strings.TrimSpace(current.UVIndex),
		}
	}

	for _, day := range payload.Weather {
		parsedDate, _ := time.Parse("2006-01-02", day.Date)
		condition, iconURL := selectForecastCondition(day.Hourly)
		snapshot.Days = append(snapshot.Days, weatherDay{
			Date:      parsedDate,
			MinTempC:  cleanTemperature(day.MintempC),
			MaxTempC:  cleanTemperature(day.MaxtempC),
			Condition: cleanWeatherText(condition, 16),
			IconURL:   iconURL,
		})
		if len(snapshot.Days) == 3 {
			break
		}
	}

	return snapshot, nil
}

func (w *WeatherWidget) attachForecastIcons(ctx context.Context, snapshot weatherSnapshot) weatherSnapshot {
	for i := range snapshot.Days {
		if snapshot.Days[i].IconURL == "" {
			continue
		}
		icon, err := w.fetchWeatherIcon(ctx, snapshot.Days[i].IconURL)
		if err != nil {
			continue
		}
		snapshot.Days[i].Icon = icon
	}

	return snapshot
}

func firstWeatherText(items []struct {
	Value string `json:"value"`
}) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].Value
}

func selectForecastCondition(hourly []struct {
	Time        string `json:"time"`
	WeatherDesc []struct {
		Value string `json:"value"`
	} `json:"weatherDesc"`
	WeatherIconURL []struct {
		Value string `json:"value"`
	} `json:"weatherIconUrl"`
}) (string, string) {
	if len(hourly) == 0 {
		return "", ""
	}

	for _, slot := range hourly {
		if strings.TrimSpace(slot.Time) == "1200" {
			return firstWeatherText(slot.WeatherDesc), firstWeatherText(slot.WeatherIconURL)
		}
	}

	mid := hourly[len(hourly)/2]
	return firstWeatherText(mid.WeatherDesc), firstWeatherText(mid.WeatherIconURL)
}

func cleanTemperature(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "--"
	}

	if _, err := strconv.Atoi(raw); err != nil {
		return "--"
	}

	return raw
}

func cleanWeatherText(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "  ", " "))
	if text == "" {
		return "Unavailable"
	}
	if len(text) <= limit {
		return text
	}
	if limit <= 1 {
		return text[:limit]
	}
	return text[:limit-1] + "."
}

func loadWeatherFaces(size int) (weatherFaces, error) {
	scale := float64(size) / 72.0
	today, err := newFace(gobold.TTF, 9.5*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather today font: %w", err)
	}
	todayDetail, err := newFace(gobold.TTF, 13.5*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather detail font: %w", err)
	}
	todayTemp, err := newFace(gobold.TTF, 16*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather temp font: %w", err)
	}
	forecastMain, err := newFace(gobold.TTF, 6.5*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather forecast main font: %w", err)
	}
	forecastDetail, err := newFace(gomono.TTF, 6*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather forecast detail font: %w", err)
	}

	return weatherFaces{
		todayDetail:    todayDetail,
		todayTemp:      todayTemp,
		today:          today,
		forecastMain:   forecastMain,
		forecastDetail: forecastDetail,
	}, nil
}

func (s *weatherViewSource) renderUnavailable(dst *image.RGBA) {
	switch s.view {
	case weatherViewToday:
		s.renderToday(dst, weatherSnapshot{
			Current: weatherCurrent{
				TempC:     "__",
				Condition: "Unavailable",
				UVIndex:   "__",
			},
			Days: []weatherDay{
				{MinTempC: "__", MaxTempC: "__"},
			},
		})
	default:
		s.renderForecast(dst, weatherSnapshot{
			Days: []weatherDay{
				{MinTempC: "__", MaxTempC: "__"},
				{MinTempC: "__", MaxTempC: "__"},
				{MinTempC: "__", MaxTempC: "__"},
			},
		})
	}
}

func (s *weatherViewSource) renderToday(dst *image.RGBA, snapshot weatherSnapshot) {
	centerX := float64(s.widget.size) / 2
	rowHeight := float64(s.widget.size) / 4
	todayRange := weatherDay{}
	if len(snapshot.Days) > 0 {
		todayRange = snapshot.Days[0]
	}

	detailFace, closeDetailFace := s.fitTodayDetailFace(snapshot.Current.Condition, float64(s.widget.size-scaledValue(s.widget.size, 8)))
	if closeDetailFace {
		defer detailFace.Close()
	}
	drawCenteredText(dst, detailFace, snapshot.Current.Condition, centerX, centeredTextBaselineY(detailFace, rowHeight*0.58), color.RGBA{R: 116, G: 236, B: 255, A: 255})
	drawSuperscriptTemperature(dst, s.faces.todayTemp, snapshot.Current.TempC, centerX, centeredTextBaselineY(s.faces.todayTemp, rowHeight*1.46), color.RGBA{R: 247, G: 248, B: 250, A: 255})
	lineY := int(rowHeight * 2)
	for x := range s.widget.size {
		dst.SetRGBA(x, lineY, color.RGBA{R: 88, G: 101, B: 118, A: 255})
	}
	drawTemperatureRange(dst, s.faces.today, todayRange.MinTempC, todayRange.MaxTempC, centerX, centeredTextBaselineY(s.faces.today, rowHeight*2.56), color.RGBA{R: 204, G: 220, B: 235, A: 255})
	drawCenteredText(dst, s.faces.today, "UV "+valueOrDash(snapshot.Current.UVIndex), centerX, centeredTextBaselineY(s.faces.today, rowHeight*3.42), color.RGBA{R: 255, G: 212, B: 109, A: 255})
}

func (s *weatherViewSource) renderForecast(dst *image.RGBA, snapshot weatherSnapshot) {
	rowHeight := s.widget.size / 3
	iconColumnWidth := int(float64(s.widget.size) * 0.34)

	for i := 0; i < 3; i++ {
		day := weatherDay{MinTempC: "__", MaxTempC: "__"}
		if i < len(snapshot.Days) {
			day = snapshot.Days[i]
		}
		rowTop := i * rowHeight
		rowBottom := rowTop + rowHeight
		iconRect := image.Rect(
			scaledValue(s.widget.size, 4),
			rowTop+scaledValue(s.widget.size, 3),
			iconColumnWidth-scaledValue(s.widget.size, 3),
			rowBottom-scaledValue(s.widget.size, 3),
		)
		drawWeatherIcon(dst, day.Icon, iconRect)

		valueCenterX := float64(iconColumnWidth) + (float64(s.widget.size-iconColumnWidth) / 2)
		drawTemperatureRange(dst, s.faces.today, day.MinTempC, day.MaxTempC, valueCenterX, centeredTextBaselineY(s.faces.today, float64(rowTop)+(float64(rowHeight)*0.56)), color.RGBA{R: 204, G: 220, B: 235, A: 255})
	}
}

func valueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "--"
	}
	return value
}

func valueOrPlaceholder(value string, placeholder string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return placeholder
	}
	return value
}

func drawWeatherIcon(dst *image.RGBA, src image.Image, rect image.Rectangle) {
	if src == nil {
		return
	}

	xdraw.CatmullRom.Scale(dst, rect, src, src.Bounds(), xdraw.Over, nil)
}

func drawSuperscriptTemperature(dst *image.RGBA, face font.Face, value string, centerX float64, baselineY float64, c color.RGBA) {
	value = valueOrPlaceholder(value, "__")
	valueWidth := measureTextWidth(face, value)
	gap := float64(scaledValue(dst.Bounds().Dx(), 1))
	degreeDiameter := float64(scaledValue(dst.Bounds().Dx(), 4))
	totalWidth := valueWidth + gap + degreeDiameter
	startX := centerX - (totalWidth / 2)

	drawCenteredText(dst, face, value, startX+(valueWidth/2), baselineY, c)
	drawDegreeMarker(
		dst,
		startX+valueWidth+gap+(degreeDiameter/2),
		baselineY-float64(scaledValue(dst.Bounds().Dx(), 6)),
		degreeDiameter/2,
		c,
	)
}

func drawTemperatureRange(dst *image.RGBA, face font.Face, minTemp string, maxTemp string, centerX float64, baselineY float64, c color.RGBA) {
	minText := valueOrPlaceholder(minTemp, "__")
	maxText := valueOrPlaceholder(maxTemp, "__")
	separator := " / "

	minWidth := measureTextWidth(face, minText)
	maxWidth := measureTextWidth(face, maxText)
	separatorWidth := measureTextWidth(face, separator)
	gap := float64(scaledValue(dst.Bounds().Dx(), 1))
	degreeDiameter := float64(scaledValue(dst.Bounds().Dx(), 3))
	totalWidth := minWidth + gap + degreeDiameter + separatorWidth + maxWidth + gap + degreeDiameter
	startX := centerX - (totalWidth / 2)
	superscriptOffset := float64(scaledValue(dst.Bounds().Dx(), 5))

	drawCenteredText(dst, face, minText, startX+(minWidth/2), baselineY, c)
	startX += minWidth + gap
	drawDegreeMarker(dst, startX+(degreeDiameter/2), baselineY-superscriptOffset, degreeDiameter/2, c)
	startX += degreeDiameter
	drawCenteredText(dst, face, separator, startX+(separatorWidth/2), baselineY, c)
	startX += separatorWidth
	drawCenteredText(dst, face, maxText, startX+(maxWidth/2), baselineY, c)
	startX += maxWidth + gap
	drawDegreeMarker(dst, startX+(degreeDiameter/2), baselineY-superscriptOffset, degreeDiameter/2, c)
}

func drawDegreeMarker(dst *image.RGBA, centerX float64, centerY float64, radius float64, c color.RGBA) {
	strokeWidth := math.Max(1, float64(scaledValue(dst.Bounds().Dx(), 1)))
	innerRadius := radius - strokeWidth
	if innerRadius < 0 {
		innerRadius = 0
	}
	drawRing(dst, centerX, centerY, radius, innerRadius, c)
}

func (s *weatherViewSource) fitTodayDetailFace(text string, maxWidth float64) (font.Face, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return s.faces.todayDetail, false
	}

	baseFace := s.faces.todayDetail
	baseWidth := measureTextWidth(baseFace, text)
	if baseWidth <= maxWidth {
		return baseFace, false
	}

	scale := float64(s.widget.size) / 72.0
	baseSize := 13.5 * scale
	minSize := 5.0 * scale
	targetSize := baseSize * (maxWidth / baseWidth)
	if targetSize > baseSize {
		targetSize = baseSize
	}
	if targetSize < minSize {
		targetSize = minSize
	}

	for size := targetSize; size >= minSize; size -= 0.25 {
		face, err := newFace(gobold.TTF, size)
		if err != nil {
			break
		}
		if measureTextWidth(face, text) <= maxWidth || size == minSize {
			return face, true
		}
		_ = face.Close()
	}

	return baseFace, false
}
