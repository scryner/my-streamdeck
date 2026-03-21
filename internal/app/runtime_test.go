package app

import "testing"

func TestResolveBrightness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings map[string]string
		want     byte
	}{
		{
			name:     "missing uses default",
			settings: map[string]string{},
			want:     runtimeBrightnessPercent,
		},
		{
			name:     "valid value",
			settings: map[string]string{"brightness": "75"},
			want:     75,
		},
		{
			name:     "too low clamps to zero",
			settings: map[string]string{"brightness": "-5"},
			want:     0,
		},
		{
			name:     "too high clamps to 100",
			settings: map[string]string{"brightness": "150"},
			want:     100,
		},
		{
			name:     "invalid uses default",
			settings: map[string]string{"brightness": "abc"},
			want:     runtimeBrightnessPercent,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveBrightness(tt.settings); got != tt.want {
				t.Fatalf("resolveBrightness() = %d, want %d", got, tt.want)
			}
		})
	}
}
