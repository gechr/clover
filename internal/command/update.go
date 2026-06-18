package command

import (
	"context"

	"github.com/gechr/clive"
	"github.com/gechr/clive/update/brew"
)

// updateTap is the Homebrew tap hosting the clover formula. Replace the
// placeholder once the tap is published.
const updateTap = "gechr/tap"

// updateCmd self-updates clover through Homebrew, the sanctioned update path.
type updateCmd struct {
	Check  bool `help:"Report whether an update is available without installing" clib:"terse='Check only'"`
	Stable bool `help:"Install the latest stable release"                        clib:"terse='Stable build'" xor:"channel"`
	Dev    bool `help:"Install the latest source build"                          clib:"terse='Dev build'"    xor:"channel"`
}

// Run checks for, or installs, the latest clover via Homebrew.
func (c *updateCmd) Run() error {
	ctx := context.Background()
	cfg := brew.Config{
		Info:    clive.Info{Module: module},
		Name:    name,
		Formula: name,
		Tap:     updateTap,
	}
	if c.Check {
		return brew.Check(ctx, cfg)
	}
	return brew.Update(ctx, cfg, brew.ChannelFor(c.Dev, c.Stable))
}
