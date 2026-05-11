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

// BottomLeftRect returns a rectangle positioned at the bottom-left of the area.
func BottomLeftRect(area uv.Rectangle, width, height int) uv.Rectangle {
	minX := area.Min.X
	maxY := area.Max.Y
	return image.Rect(minX, maxY-height, minX+width, maxY)
}
