package chat

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const maxFilename = 15

type attachmentRenderer struct {
	normalStyle, textStyle, imageStyle, deletingStyle lipgloss.Style
}

func newAttachmentRenderer(sty *styles.Styles) *attachmentRenderer {
	return &attachmentRenderer{
		normalStyle:   sty.Attachments.Normal,
		textStyle:     sty.Attachments.Text,
		imageStyle:    sty.Attachments.Image,
		deletingStyle: sty.Attachments.Deleting,
	}
}

func (r *attachmentRenderer) Render(attachments []surfacemessage.BinaryContent, deleting bool, width int) string {
	var chips []string

	maxItemWidth := lipgloss.Width(r.imageStyle.String() + r.normalStyle.Render(strings.Repeat("x", maxFilename)))
	fits := int(math.Floor(float64(width)/float64(maxItemWidth))) - 1

	for i, att := range attachments {
		filename := filepath.Base(att.Path)
		if filename == "." || filename == "/" || filename == "" {
			filename = "attachment"
		}
		if ansi.StringWidth(filename) > maxFilename {
			filename = ansi.Truncate(filename, maxFilename, "…")
		}

		if deleting {
			chips = append(chips, r.deletingStyle.Render(fmt.Sprintf("%d", i)), r.normalStyle.Render(filename))
		} else {
			chips = append(chips, r.icon(att).String(), r.normalStyle.Render(filename))
		}

		if i == fits && len(attachments) > i {
			chips = append(chips, lipgloss.NewStyle().Width(maxItemWidth).Render(fmt.Sprintf("%d more…", len(attachments)-fits)))
			break
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, chips...)
}

func (r *attachmentRenderer) icon(a surfacemessage.BinaryContent) lipgloss.Style {
	if strings.HasPrefix(a.MIMEType, "image/") {
		return r.imageStyle
	}
	return r.textStyle
}
