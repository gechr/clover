package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gechr/clive"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/auth"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/tui"
	"github.com/gechr/clover/internal/version"
	xos "github.com/gechr/x/os"
	"github.com/gechr/x/terminal"
)

// configFileName is the file clover init writes.
const configFileName = ".clover.yaml"

// configPerm is the permission for a generated config file.
const configPerm = 0o644

// initCmd scaffolds a .clover.yaml interactively.
type initCmd struct {
	Dir string `arg:"" optional:"" default:"." help:"Directory to write the config into" predictor:"dir" clib:"terse='Target directory'"`
}

// Run drives the init wizard: choose providers, report their credential status,
// then write a starter config after confirmation. It requires an interactive
// terminal, since the wizard cannot run otherwise.
func (c *initCmd) Run() error {
	if !terminal.Is(os.Stdin) || !terminal.Is(os.Stdout) {
		return errors.New("init needs an interactive terminal")
	}

	path := filepath.Join(c.Dir, configFileName)
	_, statErr := os.Stat(path)
	exists := statErr == nil

	providers, err := tui.SelectProviders(provider.Names())
	if err != nil {
		return err
	}

	settings, err := tui.Configure(tui.ConfigureInput{
		AuthSummary:     authSummary(context.Background(), providers),
		Path:            path,
		Exists:          exists,
		DefaultVersion:  defaultConstraint(),
		ValidateVersion: validateConstraint,
		ExcludeOptions:  config.CommonExcludes(),
		DefaultExcludes: config.DefaultExcludes(),
	})
	if err != nil {
		return err
	}
	if !settings.Write {
		clog.Info().Str("path", path).Msg("Left config unchanged")
		return nil
	}

	starter := config.Starter(settings.RequiredVersion, settings.Excludes)
	if err := xos.AtomicWrite(path, starter, configPerm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	clog.Info().Str("path", path).Msg("Wrote config")
	return nil
}

// defaultConstraint suggests ">=<current>" when clover is a clean tagged release,
// or "" for a dev build whose version would not make a sensible constraint.
func defaultConstraint() string {
	v, err := version.Parse(clive.Current())
	if err != nil || v.Prerelease() != "" {
		return ""
	}
	return ">=" + v.Core().String()
}

// validateConstraint reports whether s is a usable required-version constraint,
// for the wizard's live input validation. A blank value is allowed (no gate).
func validateConstraint(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// A keyword constraint (major/minor/patch) needs a current version to measure
	// against; clover's own version, or a benign fallback, serves for validation.
	current, err := version.Parse(clive.Current())
	if err != nil {
		current, _ = version.Parse("0.0.0")
	}
	if _, err := version.NewConstraint(s, current); err != nil {
		return errors.New("not a valid version constraint (e.g. >=0.1.0, ~>0.1, minor)")
	}
	return nil
}

// authSummary describes credential status for the chosen providers, for the
// wizard's note. Providers that need no credentials, or are unknown, contribute
// nothing; an all-clear (or no reportable providers) yields an empty string.
func authSummary(ctx context.Context, providers []string) string {
	var b strings.Builder
	for _, status := range auth.Check(ctx, providers) {
		if status.Authenticated {
			fmt.Fprintf(&b, "✓ %s: authenticated\n", status.Provider)
		} else {
			fmt.Fprintf(&b, "• %s: anonymous - %s\n", status.Provider, status.Hint)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
