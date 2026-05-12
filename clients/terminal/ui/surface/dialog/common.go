package dialog

import (
	uv "github.com/charmbracelet/ultraviolet"

	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	terminaltextfield "github.com/Suren878/matrixclaw/clients/terminal/ui/textfield"
)

// TextInputCursor returns a best-effort terminal cursor for a text input.
func TextInputCursor(input terminaltextfield.Model, styles surfacestyles.TextInputStyles) *uv.Cursor {
	return input.Cursor(styles)
}
