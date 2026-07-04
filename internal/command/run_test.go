package command_test

import (
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCacheEnabled(t *testing.T) {
	t.Parallel()

	off := &config.Config{Run: config.Run{Cache: new(false)}}
	tests := map[string]struct {
		flag    *bool
		noCache bool
		cfg     *config.Config
		want    bool
	}{
		"default on":              {want: true},
		"nil config defaults on":  {cfg: nil, want: true},
		"config off":              {cfg: off, want: false},
		"env wins over config on": {noCache: true, cfg: &config.Config{}, want: false},
		"flag wins over env":      {flag: new(true), noCache: true, want: true},
		"flag wins over config":   {flag: new(true), cfg: off, want: true},
		"flag off":                {flag: new(false), want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, command.CacheEnabled(tt.flag, tt.noCache, tt.cfg))
		})
	}
}
