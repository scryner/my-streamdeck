package widgets

import (
	"context"
	"image"
	"reflect"
	"sync"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

func TestWeatherWidgetTodayAndForecastUseDistinctFaces(t *testing.T) {
	t.Parallel()

	widget, err := NewWeatherWidget(WeatherWidgetOptions{
		Location: "Seoul",
	})
	if err != nil {
		t.Fatalf("NewWeatherWidget: %v", err)
	}

	today := widget.Today(streamdeck.KEY_5)
	forecast := widget.Forecast(streamdeck.KEY_6)

	if sameFace(today.source.faces.today, forecast.source.faces.today) {
		t.Fatal("expected today and forecast views to use distinct font faces")
	}
	if sameFace(today.source.faces.todayTemp, forecast.source.faces.todayTemp) {
		t.Fatal("expected today and forecast temperature faces to be distinct")
	}
}

func TestWeatherWidgetTodayAndForecastRenderConcurrently(t *testing.T) {
	t.Parallel()

	widget, err := NewWeatherWidget(WeatherWidgetOptions{
		Location: "Seoul",
		Fetch: func(context.Context, string) (weatherSnapshot, error) {
			return weatherSnapshot{
				Location: "Seoul",
				Current: weatherCurrent{
					TempC:     "9",
					Condition: "Sunny",
					UVIndex:   "3",
				},
				Days: []weatherDay{
					{Date: time.Date(2026, time.March, 21, 0, 0, 0, 0, time.UTC), MinTempC: "4", MaxTempC: "12", Condition: "Sunny"},
					{Date: time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC), MinTempC: "6", MaxTempC: "14", Condition: "Clear"},
					{Date: time.Date(2026, time.March, 23, 0, 0, 0, 0, time.UTC), MinTempC: "8", MaxTempC: "16", Condition: "Cloudy"},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWeatherWidget: %v", err)
	}

	today := widget.Today(streamdeck.KEY_5).Button()
	forecast := widget.Forecast(streamdeck.KEY_6).Button()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := today.Animation.Source.Start(ctx); err != nil {
		t.Fatalf("today Start: %v", err)
	}
	if err := forecast.Animation.Source.Start(ctx); err != nil {
		t.Fatalf("forecast Start: %v", err)
	}

	var wg sync.WaitGroup
	for _, source := range []interface {
		FrameAt(context.Context, time.Duration) (image.Image, error)
	}{today.Animation.Source, forecast.Animation.Source} {
		wg.Add(1)
		go func(source interface {
			FrameAt(context.Context, time.Duration) (image.Image, error)
		}) {
			defer wg.Done()
			for range 25 {
				frame, err := source.FrameAt(context.Background(), 0)
				if err != nil {
					t.Errorf("FrameAt: %v", err)
					return
				}
				if !frame.Bounds().Eq(image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)) {
					t.Errorf("unexpected bounds: %v", frame.Bounds())
					return
				}
			}
		}(source)
	}
	wg.Wait()
}

func TestWeatherWidgetTodayAndForecastShareCache(t *testing.T) {
	t.Parallel()

	fetchCalls := 0
	widget, err := NewWeatherWidget(WeatherWidgetOptions{
		Location: "Seoul",
		Fetch: func(context.Context, string) (weatherSnapshot, error) {
			fetchCalls++
			return weatherSnapshot{
				Location: "Seoul",
				Current: weatherCurrent{
					TempC:     "9",
					Condition: "Sunny",
					UVIndex:   "3",
				},
				Days: []weatherDay{
					{Date: time.Date(2026, time.March, 21, 0, 0, 0, 0, time.UTC), MinTempC: "4", MaxTempC: "12", Condition: "Sunny"},
					{Date: time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC), MinTempC: "6", MaxTempC: "14", Condition: "Clear"},
					{Date: time.Date(2026, time.March, 23, 0, 0, 0, 0, time.UTC), MinTempC: "8", MaxTempC: "16", Condition: "Cloudy"},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWeatherWidget: %v", err)
	}

	today := widget.Today(streamdeck.KEY_5)
	forecast := widget.Forecast(streamdeck.KEY_6)

	todayButton := today.Button()
	forecastButton := forecast.Button()
	if todayButton.Animation == nil || forecastButton.Animation == nil {
		t.Fatal("expected weather widgets to provide animations")
	}
	if todayButton.Animation.UpdateInterval != weatherViewUpdateInterval {
		t.Fatalf("expected today update interval %s, got %s", weatherViewUpdateInterval, todayButton.Animation.UpdateInterval)
	}
	if forecastButton.Animation.UpdateInterval != weatherViewUpdateInterval {
		t.Fatalf("expected forecast update interval %s, got %s", weatherViewUpdateInterval, forecastButton.Animation.UpdateInterval)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := todayButton.Animation.Source.Start(ctx); err != nil {
		t.Fatalf("today Start: %v", err)
	}
	if err := forecastButton.Animation.Source.Start(ctx); err != nil {
		t.Fatalf("forecast Start: %v", err)
	}
	if fetchCalls != 1 {
		t.Fatalf("expected shared weather cache to fetch once, got %d", fetchCalls)
	}

	todayFrame, err := todayButton.Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("today FrameAt: %v", err)
	}
	forecastFrame, err := forecastButton.Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("forecast FrameAt: %v", err)
	}

	expectedBounds := image.Rect(0, 0, DefaultClockWidgetSize, DefaultClockWidgetSize)
	if !reflect.DeepEqual(todayFrame.Bounds(), expectedBounds) {
		t.Fatalf("unexpected today bounds: %v", todayFrame.Bounds())
	}
	if !reflect.DeepEqual(forecastFrame.Bounds(), expectedBounds) {
		t.Fatalf("unexpected forecast bounds: %v", forecastFrame.Bounds())
	}

	todayPixels := 0
	forecastPixels := 0
	for y := 0; y < DefaultClockWidgetSize; y++ {
		for x := 0; x < DefaultClockWidgetSize; x++ {
			r1, g1, b1, _ := todayFrame.At(x, y).RGBA()
			if maxUint32(r1, g1, b1) > 0x7000 {
				todayPixels++
			}

			r2, g2, b2, _ := forecastFrame.At(x, y).RGBA()
			if maxUint32(r2, g2, b2) > 0x7000 {
				forecastPixels++
			}
		}
	}

	if todayPixels == 0 {
		t.Fatal("expected today weather widget to render visible content")
	}
	if forecastPixels == 0 {
		t.Fatal("expected forecast weather widget to render visible content")
	}
}

func TestWeatherWidgetRefreshNotifiesSubscribers(t *testing.T) {
	t.Parallel()

	widget, err := NewWeatherWidget(WeatherWidgetOptions{
		Location: "Seoul",
		Fetch: func(context.Context, string) (weatherSnapshot, error) {
			return weatherSnapshot{
				Location: "Seoul",
				Current: weatherCurrent{
					TempC:     "9",
					Condition: "Sunny",
					UVIndex:   "3",
				},
				Days: []weatherDay{
					{Date: time.Date(2026, time.March, 21, 0, 0, 0, 0, time.UTC), MinTempC: "4", MaxTempC: "12", Condition: "Sunny"},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWeatherWidget: %v", err)
	}

	updates := make(chan struct{}, 1)
	widget.registerUpdates(updates)
	defer widget.unregisterUpdates(updates)

	if err := widget.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	select {
	case <-updates:
	default:
		t.Fatal("expected weather refresh to notify subscribers")
	}
}

func TestWeatherForecastRendersPlaceholderWithoutData(t *testing.T) {
	t.Parallel()

	widget, err := NewWeatherWidget(WeatherWidgetOptions{
		Location: "Seoul",
		Fetch: func(context.Context, string) (weatherSnapshot, error) {
			return weatherSnapshot{}, context.DeadlineExceeded
		},
	})
	if err != nil {
		t.Fatalf("NewWeatherWidget: %v", err)
	}

	frame, err := widget.Forecast(streamdeck.KEY_6).Button().Animation.Source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}

	visiblePixels := 0
	for y := 0; y < DefaultClockWidgetSize; y++ {
		for x := 0; x < DefaultClockWidgetSize; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if maxUint32(r, g, b) > 0x7000 {
				visiblePixels++
			}
		}
	}
	if visiblePixels == 0 {
		t.Fatal("expected forecast placeholder to render visible content without data")
	}
}

func TestWeatherForecastRendersPlaceholderWhenDaysMissing(t *testing.T) {
	t.Parallel()

	widget, err := NewWeatherWidget(WeatherWidgetOptions{
		Location: "Seoul",
		Fetch: func(context.Context, string) (weatherSnapshot, error) {
			return weatherSnapshot{
				Location: "Seoul",
				Current: weatherCurrent{
					TempC:     "9",
					Condition: "Sunny",
					UVIndex:   "3",
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWeatherWidget: %v", err)
	}

	source := widget.Forecast(streamdeck.KEY_6).Button().Animation.Source
	if err := source.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	frame, err := source.FrameAt(context.Background(), 0)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}

	visiblePixels := 0
	for y := 0; y < DefaultClockWidgetSize; y++ {
		for x := 0; x < DefaultClockWidgetSize; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if maxUint32(r, g, b) > 0x7000 {
				visiblePixels++
			}
		}
	}
	if visiblePixels == 0 {
		t.Fatal("expected forecast placeholder to render visible content when days are missing")
	}
}

func TestParseWeatherSnapshot(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"current_condition": [{
			"temp_C": "9",
			"uvIndex": "3",
			"weatherDesc": [{"value": "Sunny"}]
		}],
		"weather": [
			{
				"date": "2026-03-21",
				"mintempC": "4",
				"maxtempC": "12",
				"hourly": [
					{"time": "1200", "weatherDesc": [{"value": "Sunny"}]}
				]
			},
			{
				"date": "2026-03-22",
				"mintempC": "6",
				"maxtempC": "14",
				"hourly": [
					{"time": "1200", "weatherDesc": [{"value": "Clear"}]}
				]
			},
			{
				"date": "2026-03-23",
				"mintempC": "8",
				"maxtempC": "16",
				"hourly": [
					{"time": "1200", "weatherDesc": [{"value": "Cloudy"}]}
				]
			}
		]
	}`)

	snapshot, err := parseWeatherSnapshot(body, "Seoul")
	if err != nil {
		t.Fatalf("parseWeatherSnapshot: %v", err)
	}

	if snapshot.Location != "Seoul" {
		t.Fatalf("expected location Seoul, got %q", snapshot.Location)
	}
	if snapshot.Current.TempC != "9" || snapshot.Current.Condition != "Sunny" || snapshot.Current.UVIndex != "3" {
		t.Fatalf("unexpected current snapshot: %+v", snapshot.Current)
	}
	if len(snapshot.Days) != 3 {
		t.Fatalf("expected 3 forecast days, got %d", len(snapshot.Days))
	}
	if snapshot.Days[0].MinTempC != "4" || snapshot.Days[0].MaxTempC != "12" {
		t.Fatalf("unexpected first day temps: %+v", snapshot.Days[0])
	}
}
