package chat

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const maxFilename = 15

type attachmentRenderer struct {
	normalStyle, imageStyle lipgloss.Style
}

func newAttachmentRenderer(sty *styles.Styles) *attachmentRenderer {
	return &attachmentRenderer{
		normalStyle: sty.Attachments.Normal,
		imageStyle:  sty.Attachments.Image,
	}
}

func (r *attachmentRenderer) Render(images []surfacemessage.ImageContent, width int) string {
	var chips []string

	maxItemWidth := lipgloss.Width(r.imageStyle.String() + r.normalStyle.Render(strings.Repeat("x", maxFilename)))
	fits := int(math.Floor(float64(width)/float64(maxItemWidth))) - 1

	for i, image := range images {
		filename := image.Name
		if ansi.StringWidth(filename) > maxFilename {
			filename = ansi.Truncate(filename, maxFilename, "…")
		}
		chips = append(chips, r.imageStyle.String(), r.normalStyle.Render(filename))

		if i == fits && len(images) > i {
			chips = append(chips, lipgloss.NewStyle().Width(maxItemWidth).Render(fmt.Sprintf("%d more…", len(images)-fits)))
			break
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, chips...)
}
