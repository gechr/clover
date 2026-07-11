package config_test

import (
	"testing"
	"time"

	"github.com/gechr/clover/internal/config"
	"github.com/stretchr/testify/require"
)

func TestConfig_Cooldown(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *config.Config
		want time.Duration
	}{
		"nil config": {cfg: nil, want: 0},
		"unset":      {cfg: &config.Config{}, want: 0},
		"set": {
			cfg:  &config.Config{Run: config.Run{Cooldown: new("24h")}},
			want: 24 * time.Hour,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.cfg.Cooldown())
		})
	}
}

func TestConfig_Force(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *config.Config
		want *bool
	}{
		"nil config": {cfg: nil, want: nil},
		"unset":      {cfg: &config.Config{}, want: nil},
		"true":       {cfg: &config.Config{Run: config.Run{Force: new(true)}}, want: new(true)},
		"false":      {cfg: &config.Config{Run: config.Run{Force: new(false)}}, want: new(false)},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.cfg.Force())
		})
	}
}

func TestConfig_Cache(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *config.Config
		want *bool
	}{
		"nil config": {cfg: nil, want: nil},
		"unset":      {cfg: &config.Config{}, want: nil},
		"true":       {cfg: &config.Config{Run: config.Run{Cache: new(true)}}, want: new(true)},
		"false":      {cfg: &config.Config{Run: config.Run{Cache: new(false)}}, want: new(false)},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.cfg.Cache())
		})
	}
}
