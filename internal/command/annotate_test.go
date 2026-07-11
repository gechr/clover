package command_test

import (
	"errors"
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/mode"
	"github.com/stretchr/testify/require"
)

func TestWriteFailures(t *testing.T) {
	t.Parallel()

	failed := errors.New("disk full")
	tests := map[string]struct {
		files []mode.AnnotateFile
		want  int
	}{
		"empty":       {files: nil, want: 0},
		"all written": {files: []mode.AnnotateFile{{}, {}}, want: 0},
		"all failed": {
			files: []mode.AnnotateFile{{WriteErr: failed}, {WriteErr: failed}},
			want:  2,
		},
		"mixed": {files: []mode.AnnotateFile{{}, {WriteErr: failed}, {}}, want: 1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, command.WriteFailures(mode.AnnotateSummary{Files: tc.files}))
		})
	}
}

func TestAnnotateMode(t *testing.T) {
	t.Parallel()

	cfgCheck := &config.Config{Annotate: config.Annotate{Check: new(true)}}
	cfgWrite := &config.Config{Annotate: config.Annotate{Write: new(true)}}

	tests := map[string]struct {
		check     *bool
		dryRun    *bool
		write     *bool
		cfg       *config.Config
		wantWrite bool
		wantCheck bool
	}{
		"default previews":        {wantWrite: false, wantCheck: false},
		"nil config previews":     {cfg: nil, wantWrite: false, wantCheck: false},
		"check flag":              {check: new(true), wantCheck: true},
		"dry-run flag":            {dryRun: new(true)},
		"write flag":              {write: new(true), wantWrite: true},
		"config check":            {cfg: cfgCheck, wantCheck: true},
		"config write":            {cfg: cfgWrite, wantWrite: true},
		"check beats dry-run":     {check: new(true), dryRun: new(true), wantCheck: true},
		"check beats write":       {check: new(true), write: new(true), wantCheck: true},
		"dry-run beats write":     {dryRun: new(true), write: new(true)},
		"flag beats config check": {write: new(true), cfg: cfgCheck, wantWrite: true},
		"config check beats write": {
			cfg: &config.Config{
				Annotate: config.Annotate{Check: new(true), Write: new(true)},
			},
			wantCheck: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			write, check := command.AnnotateMode(tc.check, tc.dryRun, tc.write, tc.cfg)
			require.Equal(t, tc.wantWrite, write, "write")
			require.Equal(t, tc.wantCheck, check, "check")
		})
	}
}
