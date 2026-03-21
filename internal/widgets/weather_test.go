package widgets

import (
	"context"
	"image"
	"reflect"
	"testing"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

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
