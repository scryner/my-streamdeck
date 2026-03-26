package app

import "testing"

func TestNormalizePprofAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "host and port", input: "127.0.0.1:6060", want: "127.0.0.1:6060"},
		{name: "port only", input: "6060", want: "127.0.0.1:6060"},
		{name: "empty host", input: ":7070", want: "127.0.0.1:7070"},
		{name: "invalid", input: "not-an-addr", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizePprofAddr(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePprofAddr: %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected addr: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePprofAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		enabled bool
		want    string
		wantErr bool
	}{
		{name: "disabled without env", raw: "", enabled: false, want: ""},
		{name: "enabled uses default", raw: "", enabled: true, want: defaultPprofAddr},
		{name: "env enables even without flag", raw: "6061", enabled: false, want: "127.0.0.1:6061"},
		{name: "env overrides default when enabled", raw: ":6062", enabled: true, want: "127.0.0.1:6062"},
		{name: "invalid env fails", raw: "bad", enabled: true, wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolvePprofAddr(tt.raw, tt.enabled)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePprofAddr: %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected addr: got %q want %q", got, tt.want)
			}
		})
	}
}
