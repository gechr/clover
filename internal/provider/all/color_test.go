package all_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider/all"
	colorful "github.com/lucasb-eyer/go-colorful"
	"github.com/stretchr/testify/require"
)

// minBrandDistance is the smallest perceptual gap allowed between any two
// providers' brand colors, measured as CIEDE2000 on go-colorful's normalized
// Lab scale (roughly 0-1). The palette is tuned to keep every pair comfortably
// above it on both backgrounds - lowering a color into this margin fails the
// guard below.
const minBrandDistance = 0.10

// TestProviderColorsDistinct guards the brand palette: no two providers may
// carry perceptually similar colors, on either a light or a dark terminal, so
// every provider= value reads as its own color. It doubles as the palette
// validator - retuning a color that collides with another fails here naming the
// offending pair and background.
func TestProviderColorsDistinct(t *testing.T) {
	t.Parallel()

	for _, dark := range []bool{false, true} {
		bg := "light"
		if dark {
			bg = "dark"
		}

		providers := all.New("")
		colors := make([]colorful.Color, len(providers))
		for i, p := range providers {
			c, ok := colorful.MakeColor(p.Color(dark))
			require.Truef(
				t,
				ok,
				"provider %q color is not convertible on a %s background",
				p.Name(),
				bg,
			)
			colors[i] = c
		}

		for i := range providers {
			for j := i + 1; j < len(providers); j++ {
				d := colors[i].DistanceCIEDE2000(colors[j])
				require.Greaterf(
					t,
					d,
					minBrandDistance,
					"providers %q and %q are too similar on a %s background (distance %.3f)",
					providers[i].Name(),
					providers[j].Name(),
					bg,
					d,
				)
			}
		}
	}
}
