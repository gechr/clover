package command

import (
	"context"

	"github.com/gechr/clive"
	"github.com/gechr/clive/updater/brew"
)

// updateTap is the Homebrew tap that will host the clover formula. The tap
// exists; the formula is published by the release workflow on clover's first
// public release.
const updateTap = "gechr/tap"

// cmdUpdate self-updates clover through Homebrew, the sanctioned update path.
type cmdUpdate struct {
	Check  bool `help:"Report whether an update is available without installing" clib:"terse='Check only'"`
	Stable bool `help:"Install the latest stable release"                        clib:"terse='Stable build'" xor:"channel"`
	Dev    bool `help:"Install the latest source build"                          clib:"terse='Dev build'"    xor:"channel"`
}

// updateConfig describes clover for both self-update paths: the `update` command
// (Homebrew) and the periodic check (which reads it for the display name, the
// `clover update` command, and the GitHub repo behind the tag lookup).
func updateConfig() brew.Config {
	return brew.New(
		clive.Info{Module: module},
		brew.WithName("Clover"),
		brew.WithTap(updateTap),
	)
}

// Help returns the detailed blurb shown in `clover update --help`.
func (c *cmdUpdate) Help() string {
	return "Updates Clover in place through Homebrew, the sanctioned update path. With `--check` it only reports whether a newer release is available without installing. Select a channel with `--stable` for the latest tagged release or `--dev` for the latest source build."
}

// Run checks for, or installs, the latest clover via Homebrew.
func (c *cmdUpdate) Run() error {
	ctx := context.Background()
	cfg := updateConfig()
	if c.Check {
		return brew.Check(ctx, cfg)
	}
	return brew.Update(ctx, cfg, brew.ChannelFor(c.Dev, c.Stable))
}
