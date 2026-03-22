package app

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSettingsMap(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Settings: []map[string]string{
			{"weather.location": "Yongin"},
			{"qui.access_token": "abc"},
		},
	}

	settings := cfg.SettingsMap()
	if settings["weather.location"] != "Yongin" {
		t.Fatalf("unexpected weather.location: %q", settings["weather.location"])
	}
	if settings["qui.access_token"] != "abc" {
		t.Fatalf("unexpected qui.access_token: %q", settings["qui.access_token"])
	}
}

func TestDefaultConfigContainsOnlyWidgetsWithoutRequiredSettings(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if len(cfg.ButtonWidgets) != 4 {
		t.Fatalf("expected 4 default button widgets, got %d", len(cfg.ButtonWidgets))
	}

	got := []string{
		cfg.ButtonWidgets[0].Type,
		cfg.ButtonWidgets[1].Type,
		cfg.ButtonWidgets[2].Type,
		cfg.ButtonWidgets[3].Type,
	}
	want := []string{"clock", "calendar", "sysstat", "caffeinate"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected widget order: got %v want %v", got, want)
		}
	}

	if len(cfg.TouchWidgets) != 4 {
		t.Fatalf("expected 4 default touch widgets, got %d", len(cfg.TouchWidgets))
	}
	gotTouch := []string{
		cfg.TouchWidgets[0].Type,
		cfg.TouchWidgets[1].Type,
		cfg.TouchWidgets[2].Type,
		cfg.TouchWidgets[3].Type,
	}
	wantTouch := []string{"playback", "brightness", "microphone", "volume"}
	for i := range wantTouch {
		if gotTouch[i] != wantTouch[i] {
			t.Fatalf("unexpected touch widget order: got %v want %v", gotTouch, wantTouch)
		}
	}
}

func TestConfigLegacyWidgetsFallback(t *testing.T) {
	t.Parallel()

	var cfg Config
	data := []byte(`
widgets:
  - type: clock
    first: analog
  - type: calendar
`)
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if len(cfg.ButtonWidgets) != 0 {
		t.Fatalf("expected direct button_widgets to be empty, got %d", len(cfg.ButtonWidgets))
	}
	if len(cfg.LegacyWidgets) != 2 {
		t.Fatalf("expected 2 legacy widgets, got %d", len(cfg.LegacyWidgets))
	}
	if len(cfg.ButtonWidgets) == 0 && len(cfg.LegacyWidgets) > 0 {
		cfg.ButtonWidgets = append([]ButtonWidgetConfig(nil), cfg.LegacyWidgets...)
	}
	if len(cfg.ButtonWidgets) != 2 {
		t.Fatalf("expected fallback button widgets, got %d", len(cfg.ButtonWidgets))
	}
	if cfg.ButtonWidgets[0].Type != "clock" || cfg.ButtonWidgets[1].Type != "calendar" {
		t.Fatalf("unexpected fallback widgets: %+v", cfg.ButtonWidgets)
	}
}
