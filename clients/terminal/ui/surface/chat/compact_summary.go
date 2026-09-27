package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
)

type contextMarkerKind string

const (
	contextMarkerCompact contextMarkerKind = "compact"
	contextMarkerClear   contextMarkerKind = "clear"
)

type CompactSummaryMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	id      string
	kind    contextMarkerKind
	content string
	stats   string
	sty     *surfacestyles.Styles
}

func NewCompactSummaryMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message) *CompactSummaryMessageItem {
	return newContextMarkerMessageItem(sty, message, contextMarkerCompact)
}

func NewContextClearedMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message) *CompactSummaryMessageItem {
	return newContextMarkerMessageItem(sty, message, contextMarkerClear)
}

func newContextMarkerMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message, kind contextMarkerKind) *CompactSummaryMessageItem {
	var boundary surfacemessage.ContextBoundary
	if message.Boundary != nil {
		boundary = *message.Boundary
	}
	return &CompactSummaryMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		id:                       message.ID + ":" + kind.idSuffix(),
		kind:                     kind,
		content:                  strings.TrimSpace(boundary.Summary),
		stats:                    compactSummaryStats(boundary),
		sty:                      sty,
	}
}

func IsCompactSummaryMessage(message *surfacemessage.Message) bool {
	return message != nil && message.Boundary != nil && !message.Boundary.Cleared
}

func IsContextClearedMessage(message *surfacemessage.Message) bool {
	return message != nil && message.Boundary != nil && message.Boundary.Cleared
}

func (c *CompactSummaryMessageItem) ID() string {
	return c.id
}

func (c *CompactSummaryMessageItem) RawRender(width int) string {
	innerWidth := cappedMessageWidth(width)
	content, height, ok := c.getCachedRender(innerWidth)
	if !ok {
		content = c.renderContent(innerWidth)
		height = lipgloss.Height(content)
		c.setCachedRender(content, innerWidth, height)
	}
	return c.renderHighlighted(content, innerWidth, height)
}

func (c *CompactSummaryMessageItem) Render(width int) string {
	return renderUnifiedMessageLines(c.sty, c.RawRender(width), c.focused, c.sty.Chat.Message.ToolMarker)
}

func (c *CompactSummaryMessageItem) HandleKeyEvent(key tea.KeyPressMsg) (bool, tea.Cmd) {
	switch key.String() {
	case "enter", "v":
	default:
		return false, nil
	}
	if c.content == "" {
		return false, nil
	}
	return true, func() tea.Msg {
		return surfacedialog.ActionOpenFilePreview{Data: surfacedialog.FilePreviewData{
			Title:   c.kind.previewTitle(),
			Content: c.content,
		}}
	}
}

func (c *CompactSummaryMessageItem) renderContent(width int) string {
	parts := []string{toolNameStyle(c.sty, false).Render(c.kind.label())}
	if c.stats != "" {
		parts = append(parts, c.sty.Tool.ParamMain.Render(c.stats))
	}
	if c.content != "" {
		parts = append(parts, c.sty.Muted.Render("press enter to view"))
	}
	line := strings.Join(parts, " ")
	if width >= 0 {
		line = ansi.Truncate(line, width, "…")
	}
	return line
}

// compactSummaryStats shows how far a compaction shrank the context.
func compactSummaryStats(boundary surfacemessage.ContextBoundary) string {
	if boundary.Cleared || boundary.TokensBefore <= 0 {
		return ""
	}
	return fmt.Sprintf("(~%s -> ~%s tokens)", agentcontext.FormatShortNumber(boundary.TokensBefore), agentcontext.FormatShortNumber(boundary.TokensAfter))
}

func (k contextMarkerKind) idSuffix() string {
	switch k {
	case contextMarkerClear:
		return "context-clear"
	default:
		return "compact-summary"
	}
}

func (k contextMarkerKind) label() string {
	switch k {
	case contextMarkerClear:
		return "Context cleared"
	default:
		return "Context compacted"
	}
}

func (k contextMarkerKind) previewTitle() string {
	switch k {
	case contextMarkerClear:
		return "Context Clear"
	default:
		return "Context Summary"
	}
}
