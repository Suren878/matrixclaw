package common

import (
	"image"

	uv "github.com/charmbracelet/ultraviolet"

	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

type Common struct {
	Styles *surfacestyles.Styles
}

func DefaultCommon() *Common {
	styles := surfacestyles.DefaultStyles()
	return &Common{Styles: &styles}
}

// CenterRect returns a rectangle centered within the given area.
func CenterRect(area uv.Rectangle, width, height int) uv.Rectangle {
	centerX := area.Min.X + area.Dx()/2
	centerY := area.Min.Y + area.Dy()/2
	minX := centerX - width/2
	minY := centerY - height/2
	return image.Rect(minX, minY, minX+width, minY+height)
}
