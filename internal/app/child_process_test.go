package app

import (
	"reflect"
	"testing"
)

func TestChildProcessArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts RunOptions
		want []string
	}{
		{
			name: "spawn only",
			opts: RunOptions{},
			want: []string{"--spawn"},
		},
		{
			name: "propagates verbose and pprof",
			opts: RunOptions{
				EnablePprof: true,
				Verbose:     true,
			},
			want: []string{"--spawn", "--pprof", "--verbose"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := childProcessArgs(tt.opts); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("childProcessArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}
