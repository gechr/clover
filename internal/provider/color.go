package provider

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Adapt picks a provider's brand color for the terminal background: the dark
// variant on a dark terminal, the light variant otherwise. Each provider's
// Color method delegates here so the two brand hexes live at the call site and
// the light/dark choice stays in one place. Both arguments are hex strings
// (#rrggbb).
func Adapt(dark bool, light, darkVariant string) color.Color {
	if dark {
		return lipgloss.Color(darkVariant)
	}
	return lipgloss.Color(light)
}
