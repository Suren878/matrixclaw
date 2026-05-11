package diffview

import (
	"image/color"

	"github.com/charmbracelet/x/exp/charmtone"
)

func tone(key charmtone.Key) color.Color {
	return key
}

func gloss(c color.Color) color.Color {
	return c
}
