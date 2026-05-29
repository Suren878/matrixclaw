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
)

const (
	compactSummaryPrefix = "🧠 Context compacted"
	contextClearedPrefix = "🧹 Context cleared"
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
	sty     *surfacestyles.Styles
}

func NewCompactSummaryMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message) *CompactSummaryMessageItem {
	return newContextMarkerMessageItem(sty, message, contextMarkerCompact)
}

func NewContextClearedMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message) *CompactSummaryMessageItem {
	return newContextMarkerMessageItem(sty, message, contextMarkerClear)
}

func newContextMarkerMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message, kind contextMarkerKind) *CompactSummaryMessageItem {
	return &CompactSummaryMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		id:                       message.ID + ":" + kind.idSuffix(),
		kind:                     kind,
		content:                  strings.TrimSpace(message.Content().Text),
		sty:                      sty,
	}
}

func IsCompactSummaryMessage(message *surfacemessage.Message) bool {
	if message == nil || message.Role != surfacemessage.System {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(message.Content().Text), compactSummaryPrefix)
}

func IsContextClearedMessage(message *surfacemessage.Message) bool {
	if message == nil || message.Role != surfacemessage.System {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(message.Content().Text), contextClearedPrefix)
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
	return renderUnifiedMessageLines(c.sty, c.RawRender(width), c.focused, "●", c.sty.Chat.Message.ToolMarker)
}

func (c *CompactSummaryMessageItem) HandleKeyEvent(key tea.KeyPressMsg) (bool, tea.Cmd) {
	switch key.String() {
	case "enter", "v":
	default:
		return false, nil
	}
	content := strings.TrimSpace(c.content)
	if content == "" {
		return false, nil
	}
	return true, func() tea.Msg {
		return surfacedialog.ActionOpenFilePreview{Data: surfacedialog.FilePreviewData{
			Title:   c.kind.previewTitle(),
			Content: content,
		}}
	}
}

func (c *CompactSummaryMessageItem) renderContent(width int) string {
	stats := ""
	if c.kind == contextMarkerCompact {
		stats = compactSummaryStats(c.content)
	}
	name := toolNameStyle(c.sty, false).Render(c.kind.label())
	parts := []string{name}
	if stats != "" {
		parts = append(parts, c.sty.Tool.ParamMain.Render(stats))
	}
	parts = append(parts, c.sty.Muted.Render("press enter to view"))
	line := strings.Join(parts, " ")
	if width >= 0 {
		line = ansi.Truncate(line, width, "…")
	}
	return line
}

func compactSummaryStats(content string) string {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	firstLine = strings.TrimSpace(strings.TrimPrefix(firstLine, compactSummaryPrefix))
	firstLine = strings.TrimPrefix(firstLine, ":")
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return ""
	}
	return fmt.Sprintf("(%s)", firstLine)
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
