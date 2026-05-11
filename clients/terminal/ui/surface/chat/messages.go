package chat

import (
	"fmt"
	"image"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/anim"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/list"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const (
	MessageLeftPaddingTotal        = 2
	maxTextWidth                   = 120
	minTextRenderWidth             = 20
	assistantMessageTruncateFormat = "… (%d lines hidden) [click or space to expand]"
)

type Identifiable interface {
	ID() string
}

type Animatable interface {
	StartAnimation() tea.Cmd
	Animate(msg anim.StepMsg) tea.Cmd
}

type Expandable interface {
	ToggleExpanded() bool
}

type KeyEventHandler interface {
	HandleKeyEvent(key tea.KeyPressMsg) (bool, tea.Cmd)
}

type MessageItem interface {
	list.Item
	list.RawRenderable
	Identifiable
}

type HighlightableMessageItem interface {
	MessageItem
	list.Highlightable
}

type FocusableMessageItem interface {
	MessageItem
	list.Focusable
}

type highlightableMessageItem struct {
	startLine   int
	startCol    int
	endLine     int
	endCol      int
	highlighter list.Highlighter
}

func (h *highlightableMessageItem) isHighlighted() bool {
	return h.startLine != -1 || h.endLine != -1
}

func (h *highlightableMessageItem) renderHighlighted(content string, width, height int) string {
	if !h.isHighlighted() {
		return content
	}
	area := image.Rect(0, 0, width, height)
	return list.Highlight(content, area, h.startLine, h.startCol, h.endLine, h.endCol, h.highlighter)
}

func (h *highlightableMessageItem) SetHighlight(startLine int, startCol int, endLine int, endCol int) {
	offset := MessageLeftPaddingTotal
	h.startLine = startLine
	h.startCol = max(0, startCol-offset)
	h.endLine = endLine
	if endCol >= 0 {
		h.endCol = max(0, endCol-offset)
	} else {
		h.endCol = endCol
	}
}

func (h *highlightableMessageItem) Highlight() (startLine int, startCol int, endLine int, endCol int) {
	return h.startLine, h.startCol, h.endLine, h.endCol
}

func defaultHighlighter(sty *surfacestyles.Styles) *highlightableMessageItem {
	return &highlightableMessageItem{
		startLine:   -1,
		startCol:    -1,
		endLine:     -1,
		endCol:      -1,
		highlighter: list.ToHighlighter(sty.TextSelection),
	}
}

func renderUnifiedMessageLines(sty *surfacestyles.Styles, rendered string, focused bool, marker string, markerStyle lipgloss.Style) string {
	lines := strings.Split(rendered, "\n")
	firstContentLine := true
	for i, line := range lines {
		lineMarker := " "
		if strings.TrimSpace(line) != "" && firstContentLine {
			lineMarker = marker
			firstContentLine = false
		}
		markerCell := markerStyle.Inline(true).Width(1).Render(lineMarker)
		if focused {
			markerCell = sty.Chat.Message.FocusedMarker.Inline(true).Width(1).Render(" ")
		}
		lines[i] = markerCell + " " + line
	}
	return strings.Join(lines, "\n")
}

func renderUserMessageLines(sty *surfacestyles.Styles, rendered string, focused bool, width int) string {
	lines := strings.Split(rendered, "\n")
	output := make([]string, 0, len(lines)+2)
	output = append(output, renderUserMessageBody(sty, focused, " ", "", width))
	firstContentLine := true
	for _, line := range lines {
		lineMarker := " "
		if strings.TrimSpace(line) != "" && firstContentLine {
			lineMarker = ">"
			firstContentLine = false
		}
		output = append(output, renderUserMessageBody(sty, focused, lineMarker, line, width))
	}
	output = append(output, renderUserMessageBody(sty, focused, " ", "", width))
	return strings.Join(output, "\n")
}

func renderUserMessageBody(sty *surfacestyles.Styles, focused bool, marker, line string, width int) string {
	if width <= 0 {
		return ""
	}
	markerStyle := sty.Chat.Message.UserMarker
	if focused {
		markerStyle = sty.Chat.Message.FocusedMarker
		marker = " "
	}
	if width == 1 {
		return markerStyle.Inline(true).Width(1).Render(marker)
	}

	markerCell := markerStyle.Inline(true).Width(1).Render(marker)
	spaceCell := sty.Chat.Message.FocusedLine.Inline(true).Width(1).Render(" ")
	textWidth := max(0, width-2)
	text := ansi.Truncate(ansi.Strip(line), textWidth, "…")
	textCell := sty.Chat.Message.FocusedLine.Width(textWidth).Render(text)
	return markerCell + spaceCell + textCell
}

type cachedMessageItem struct {
	rendered string
	width    int
	height   int
}

func (c *cachedMessageItem) getCachedRender(width int) (string, int, bool) {
	if c.width == width && c.rendered != "" {
		return c.rendered, c.height, true
	}
	return "", 0, false
}

func (c *cachedMessageItem) setCachedRender(rendered string, width, height int) {
	c.rendered = rendered
	c.width = width
	c.height = height
}

func (c *cachedMessageItem) clearCache() {
	c.rendered = ""
	c.width = 0
	c.height = 0
}

type focusableMessageItem struct {
	focused bool
}

func (f *focusableMessageItem) SetFocused(focused bool) {
	f.focused = focused
}

type AssistantInfoItem struct {
	*cachedMessageItem

	id                  string
	message             *surfacemessage.Message
	sty                 *surfacestyles.Styles
	lastUserMessageTime time.Time
}

func NewAssistantInfoItem(sty *surfacestyles.Styles, message *surfacemessage.Message, lastUserMessageTime time.Time) MessageItem {
	return &AssistantInfoItem{
		cachedMessageItem:   &cachedMessageItem{},
		id:                  AssistantInfoID(message.ID),
		message:             message,
		sty:                 sty,
		lastUserMessageTime: lastUserMessageTime,
	}
}

func (a *AssistantInfoItem) ID() string {
	return a.id
}

func (a *AssistantInfoItem) RawRender(width int) string {
	innerWidth := max(0, width-MessageLeftPaddingTotal)
	content, _, ok := a.getCachedRender(innerWidth)
	if !ok {
		content = a.renderContent(innerWidth)
		height := lipgloss.Height(content)
		a.setCachedRender(content, innerWidth, height)
	}
	return content
}

func (a *AssistantInfoItem) Render(width int) string {
	prefix := a.sty.Chat.Message.SectionHeader.Render()
	lines := strings.Split(a.RawRender(width), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func (a *AssistantInfoItem) renderContent(width int) string {
	finishData := a.message.FinishPart()
	if finishData == nil {
		return ""
	}
	finishTime := time.Unix(finishData.Time, 0)
	duration := finishTime.Sub(a.lastUserMessageTime)
	if a.lastUserMessageTime.IsZero() || duration < 0 {
		duration = 0
	}
	infoMsg := a.sty.Chat.Message.AssistantInfoDuration.Render(duration.String())
	icon := a.sty.Chat.Message.AssistantInfoIcon.Render(surfacestyles.ModelIcon)
	modelName := strings.TrimSpace(a.message.Model)
	if modelName == "" {
		modelName = "Unknown Model"
	}
	modelFormatted := a.sty.Chat.Message.AssistantInfoModel.Render(modelName)
	providerName := strings.TrimSpace(a.message.Provider)
	var provider string
	if providerName != "" {
		provider = a.sty.Chat.Message.AssistantInfoProvider.Render(fmt.Sprintf("via %s", providerName))
	}
	parts := []string{icon, modelFormatted}
	if provider != "" {
		parts = append(parts, provider)
	}
	parts = append(parts, infoMsg)
	return common.Section(a.sty, strings.Join(parts, " "), width)
}

func BuildToolResultMap(messages []surfacemessage.Message) map[string]surfacemessage.ToolResult {
	resultMap := make(map[string]surfacemessage.ToolResult)
	for _, msg := range messages {
		if msg.Role != surfacemessage.Tool {
			continue
		}
		for _, result := range msg.ToolResults() {
			if result.ToolCallID != "" {
				resultMap[result.ToolCallID] = result
			}
		}
	}
	return resultMap
}

func ExtractMessageItems(sty *surfacestyles.Styles, msg *surfacemessage.Message, toolResults map[string]surfacemessage.ToolResult) []MessageItem {
	switch msg.Role {
	case surfacemessage.User:
		return []MessageItem{NewUserMessageItem(sty, msg)}
	case surfacemessage.Assistant, surfacemessage.System:
		var items []MessageItem
		if ShouldRenderAssistantMessage(msg) {
			items = append(items, NewAssistantMessageItem(sty, msg))
		}
		for _, tc := range msg.ToolCalls() {
			var result *surfacemessage.ToolResult
			if tr, ok := toolResults[tc.ID]; ok {
				result = &tr
			}
			items = append(items, NewToolMessageItem(
				sty,
				msg.ID,
				tc,
				result,
				msg.FinishReason() == surfacemessage.FinishReasonCanceled,
			))
		}
		return items
	}
	return nil
}

func ShouldRenderAssistantMessage(msg *surfacemessage.Message) bool {
	content := strings.TrimSpace(msg.Content().Text)
	thinking := strings.TrimSpace(msg.ReasoningContent().Thinking)
	isError := msg.FinishReason() == surfacemessage.FinishReasonError
	isCancelled := msg.FinishReason() == surfacemessage.FinishReasonCanceled
	hasToolCalls := len(msg.ToolCalls()) > 0
	return !hasToolCalls || content != "" || thinking != "" || msg.IsThinking() || isError || isCancelled
}

func AssistantInfoID(messageID string) string {
	return fmt.Sprintf("%s:assistant-info", messageID)
}

func cappedMessageWidth(availableWidth int) int {
	width := availableWidth - MessageLeftPaddingTotal
	if width < minTextRenderWidth {
		width = minTextRenderWidth
	}
	return min(width, maxTextWidth)
}
