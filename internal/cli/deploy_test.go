package cli

import (
	"testing"

	"github.com/ivantit66/onebase/internal/project"
)

func TestDeployVersionMessage(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		explicit string
		cfg      *project.AppConfig
		want     string
	}{
		{
			name:     "explicit message wins",
			dir:      "/tmp/trade",
			explicit: "  release 2.0.0  ",
			cfg:      &project.AppConfig{Name: "Trade", Version: "1.0.0"},
			want:     "release 2.0.0",
		},
		{
			name: "app name and version",
			dir:  "/tmp/trade",
			cfg:  &project.AppConfig{Name: "Trade", Version: "1.4.0"},
			want: "release Trade 1.4.0",
		},
		{
			name: "version without name",
			dir:  "/tmp/trade",
			cfg:  &project.AppConfig{Version: "1.4.0"},
			want: "release 1.4.0",
		},
		{
			name: "name without version",
			dir:  "/tmp/trade",
			cfg:  &project.AppConfig{Name: "Trade"},
			want: "release Trade",
		},
		{
			name: "directory fallback",
			dir:  "/tmp/trade",
			want: "release trade",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deployVersionMessage(tt.dir, tt.explicit, tt.cfg); got != tt.want {
				t.Fatalf("deployVersionMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
