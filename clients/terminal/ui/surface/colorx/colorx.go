// Package colorx keeps Lip Gloss v2 color conversion consistent across the
// terminal UI.
package colorx

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

func Hex(c color.Color) string {
	if c == nil {
		return ""
	}
	if _, ok := c.(lipgloss.NoColor); ok {
		return ""
	}
	if key, ok := c.(charmtone.Key); ok {
		return key.Hex()
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}
