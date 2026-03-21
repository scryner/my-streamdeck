package widgets

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"rafaelmartins.com/p/streamdeck"
)

const (
	weatherCacheRefreshInterval = 10 * time.Minute
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
	location   string
	size       int
	fetch      WeatherFetchFunc
	now        func() time.Time
	httpClient *http.Client
	faces      weatherFaces

	startOnce sync.Once

	stateMu sync.RWMutex
	cached  weatherSnapshot
	hasData bool
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
	widget *WeatherWidget
	view   weatherViewKind
}

type weatherViewKind int

const (
	weatherViewToday weatherViewKind = iota
	weatherViewForecast
)

type weatherFaces struct {
	title          font.Face
	temp           font.Face
	detail         font.Face
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
		} `json:"hourly"`
	} `json:"weather"`
}

var (
	weatherFacesMu    sync.Mutex
	weatherFacesCache = map[int]weatherFaces{}
)

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

	faces, err := loadWeatherFaces(options.Size)
	if err != nil {
		return nil, err
	}

	widget := &WeatherWidget{
		location:   options.Location,
		size:       options.Size,
		now:        options.Now,
		httpClient: options.HTTPClient,
		faces:      faces,
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
		},
	}
}

func (w *WeatherWidget) Forecast(key streamdeck.KeyID) *WeatherForecastWidget {
	return &WeatherForecastWidget{
		key: key,
		source: &weatherViewSource{
			widget: w,
			view:   weatherViewForecast,
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
	return s.widget.start(ctx)
}

func (s *weatherViewSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	img := image.NewRGBA(image.Rect(0, 0, s.widget.size, s.widget.size))
	snapshot, ok := s.widget.currentSnapshot()
	if !ok {
		s.renderLoading(img)
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

func (s *weatherViewSource) Close() error {
	return nil
}

func (w *WeatherWidget) start(ctx context.Context) error {
	w.startOnce.Do(func() {
		_ = w.refresh(ctx)

		go func() {
			ticker := time.NewTicker(weatherCacheRefreshInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					_ = w.refresh(ctx)
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
	return nil
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

	return parseWeatherSnapshot(body, location)
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
		snapshot.Days = append(snapshot.Days, weatherDay{
			Date:      parsedDate,
			MinTempC:  cleanTemperature(day.MintempC),
			MaxTempC:  cleanTemperature(day.MaxtempC),
			Condition: cleanWeatherText(selectForecastCondition(day.Hourly), 16),
		})
		if len(snapshot.Days) == 3 {
			break
		}
	}

	return snapshot, nil
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
}) string {
	if len(hourly) == 0 {
		return ""
	}

	for _, slot := range hourly {
		if strings.TrimSpace(slot.Time) == "1200" {
			return firstWeatherText(slot.WeatherDesc)
		}
	}

	return firstWeatherText(hourly[len(hourly)/2].WeatherDesc)
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
	weatherFacesMu.Lock()
	defer weatherFacesMu.Unlock()

	if faces, ok := weatherFacesCache[size]; ok {
		return faces, nil
	}

	scale := float64(size) / 72.0
	title, err := newFace(gobold.TTF, 7.5*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather title font: %w", err)
	}
	temp, err := newFace(gobold.TTF, 18*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather temp font: %w", err)
	}
	detail, err := newFace(gomono.TTF, 7.5*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather detail font: %w", err)
	}
	forecastMain, err := newFace(gobold.TTF, 6.5*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather forecast main font: %w", err)
	}
	forecastDetail, err := newFace(gomono.TTF, 6*scale)
	if err != nil {
		return weatherFaces{}, fmt.Errorf("load weather forecast detail font: %w", err)
	}

	faces := weatherFaces{
		title:          title,
		temp:           temp,
		detail:         detail,
		forecastMain:   forecastMain,
		forecastDetail: forecastDetail,
	}
	weatherFacesCache[size] = faces
	return faces, nil
}

func (s *weatherViewSource) renderLoading(dst *image.RGBA) {
	fillVerticalGradient(dst, color.RGBA{R: 18, G: 23, B: 33, A: 255}, color.RGBA{R: 10, G: 14, B: 24, A: 255})
	drawCenteredText(dst, s.widget.faces.title, stringsUpper(s.widget.location), float64(s.widget.size)/2, float64(scaledValue(s.widget.size, 12)), color.RGBA{R: 135, G: 201, B: 255, A: 255})
	drawCenteredText(dst, s.widget.faces.temp, "Loading", float64(s.widget.size)/2, centeredTextBaselineY(s.widget.faces.temp, float64(s.widget.size)*0.48), color.RGBA{R: 244, G: 246, B: 248, A: 255})
}

func (s *weatherViewSource) renderToday(dst *image.RGBA, snapshot weatherSnapshot) {
	fillVerticalGradient(dst, color.RGBA{R: 19, G: 32, B: 52, A: 255}, color.RGBA{R: 10, G: 18, B: 32, A: 255})

	centerX := float64(s.widget.size) / 2
	drawCenteredText(dst, s.widget.faces.title, stringsUpper(snapshot.Location), centerX, float64(scaledValue(s.widget.size, 12)), color.RGBA{R: 145, G: 217, B: 255, A: 255})
	drawCenteredText(dst, s.widget.faces.temp, snapshot.Current.TempC+"C", centerX, centeredTextBaselineY(s.widget.faces.temp, float64(s.widget.size)*0.42), color.RGBA{R: 247, G: 248, B: 250, A: 255})
	drawCenteredText(dst, s.widget.faces.detail, snapshot.Current.Condition, centerX, centeredTextBaselineY(s.widget.faces.detail, float64(s.widget.size)*0.68), color.RGBA{R: 210, G: 226, B: 241, A: 255})
	drawCenteredText(dst, s.widget.faces.detail, "UV "+valueOrDash(snapshot.Current.UVIndex), centerX, centeredTextBaselineY(s.widget.faces.detail, float64(s.widget.size)*0.83), color.RGBA{R: 255, G: 212, B: 109, A: 255})
}

func (s *weatherViewSource) renderForecast(dst *image.RGBA, snapshot weatherSnapshot) {
	fillVerticalGradient(dst, color.RGBA{R: 16, G: 22, B: 34, A: 255}, color.RGBA{R: 9, G: 13, B: 22, A: 255})

	rowHeight := s.widget.size / 3
	for i := range 2 {
		lineY := (i + 1) * rowHeight
		for x := range s.widget.size {
			dst.SetRGBA(x, lineY, color.RGBA{R: 71, G: 81, B: 97, A: 255})
		}
	}

	for i := 0; i < 3; i++ {
		if i >= len(snapshot.Days) {
			break
		}

		day := snapshot.Days[i]
		centerY := float64(i*rowHeight) + (float64(rowHeight) / 2)
		topLine := fmt.Sprintf("%s %sC/%sC", weekdayLabel(day.Date), day.MinTempC, day.MaxTempC)
		drawCenteredText(dst, s.widget.faces.forecastMain, topLine, float64(s.widget.size)/2, centeredTextBaselineY(s.widget.faces.forecastMain, centerY-8), color.RGBA{R: 240, G: 243, B: 247, A: 255})
		drawCenteredText(dst, s.widget.faces.forecastDetail, day.Condition, float64(s.widget.size)/2, centeredTextBaselineY(s.widget.faces.forecastDetail, centerY+9), color.RGBA{R: 173, G: 194, B: 214, A: 255})
	}
}

func weekdayLabel(date time.Time) string {
	if date.IsZero() {
		return "---"
	}
	return stringsUpper(date.Weekday().String()[:3])
}

func valueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "--"
	}
	return value
}
