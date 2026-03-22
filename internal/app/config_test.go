package app

import "testing"

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
}
