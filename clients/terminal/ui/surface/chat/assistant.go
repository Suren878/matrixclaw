package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	termmarkdown "github.com/MichaelMure/go-term-markdown"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const (
	goTermMarkdownInlineCodeBlueBg = "\x1b[44;3m"
	assistantInlineCodeCyan        = "\x1b[36m"
	goTermMarkdownGreenBullet      = "\x1b[32m• \x1b[0m"
	goTermMarkdownWhiteBullet      = "\x1b[97m• \x1b[0m"
	assistantDashBullet            = "\x1b[97m- \x1b[0m"
)

type AssistantMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	message *surfacemessage.Message
	sty     *surfacestyles.Styles
}

func NewAssistantMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message) MessageItem {
	return &AssistantMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		message:                  message,
		sty:                      sty,
	}
}

func (a *AssistantMessageItem) ID() string {
	return a.message.ID
}

func (a *AssistantMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)

	content, height, ok := a.getCachedRender(cappedWidth)
	if !ok {
		content = a.renderMessageContent(cappedWidth)
		height = lipgloss.Height(content)
		a.setCachedRender(content, cappedWidth, height)
	}

	return a.renderHighlighted(content, cappedWidth, height)
}

func (a *AssistantMessageItem) Render(width int) string {
	return renderUnifiedMessageLines(a.sty, a.RawRender(width), a.focused, a.sty.Chat.Message.AssistantMarker)
}

func (a *AssistantMessageItem) renderMessageContent(width int) string {
	var messageParts []string
	content := strings.TrimSpace(a.message.Content().Text)

	if content != "" {
		if a.message.IsSummaryMessage {
			messageParts = append(messageParts, a.renderPlainText(content, width))
		} else {
			messageParts = append(messageParts, a.renderMarkdown(content, width))
		}
	}

	if a.message.IsFinished() {
		switch a.message.FinishReason() {
		case surfacemessage.FinishReasonCanceled:
			messageParts = append(messageParts, a.sty.Base.Italic(true).Render("Canceled"))
		case surfacemessage.FinishReasonError:
			messageParts = append(messageParts, a.renderError(width))
		}
	}

	return strings.Join(messageParts, "\n")
}

func (a *AssistantMessageItem) renderMarkdown(content string, width int) string {
	width = safeTextRenderWidth(width)
	result := termmarkdown.Render(content, width, 0)
	rendered := normalizeAssistantMarkdownANSI(string(result))
	rendered = common.HighlightPlainPaths(rendered, a.sty)
	return strings.TrimRight(rendered, "\n")
}

func (a *AssistantMessageItem) renderPlainText(content string, width int) string {
	width = safeTextRenderWidth(width)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " ")
		if line == "" {
			out = append(out, "")
			continue
		}
		wrapped := strings.TrimRight(ansi.Wrap(line, width, " \t/\\._:=,;|-"), "\n")
		out = append(out, strings.Split(wrapped, "\n")...)
	}
	rendered := strings.Join(out, "\n")
	rendered = common.HighlightPlainPaths(rendered, a.sty)
	return strings.TrimRight(rendered, "\n")
}

func normalizeAssistantMarkdownANSI(rendered string) string {
	rendered = strings.ReplaceAll(rendered, goTermMarkdownInlineCodeBlueBg, assistantInlineCodeCyan)
	rendered = strings.ReplaceAll(rendered, goTermMarkdownGreenBullet, assistantDashBullet)
	rendered = strings.ReplaceAll(rendered, goTermMarkdownWhiteBullet, assistantDashBullet)
	rendered = strings.ReplaceAll(rendered, "• ", "- ")
	return rendered
}

func (a *AssistantMessageItem) renderError(width int) string {
	width = safeTextRenderWidth(width)
	finishPart := a.message.FinishPart()
	if finishPart == nil {
		return ""
	}
	errTag := a.sty.Chat.Message.ErrorTag.Render("ERROR")
	truncated := ansi.Truncate(finishPart.Message, width-2-lipgloss.Width(errTag), "...")
	title := fmt.Sprintf("%s %s", errTag, a.sty.Chat.Message.ErrorTitle.Render(truncated))
	details := a.sty.Chat.Message.ErrorDetails.Width(width - 2).Render(finishPart.Details)
	return fmt.Sprintf("%s\n\n%s", title, details)
}

func (a *AssistantMessageItem) HandleKeyEvent(key tea.KeyPressMsg) (bool, tea.Cmd) {
	switch key.String() {
	case "enter", "v":
	default:
		return false, nil
	}
	data, ok := a.errorPreviewData()
	if !ok {
		return false, nil
	}
	return true, func() tea.Msg {
		return surfacedialog.ActionOpenFilePreview{Data: data}
	}
}

func (a *AssistantMessageItem) errorPreviewData() (surfacedialog.FilePreviewData, bool) {
	if a.message.FinishReason() != surfacemessage.FinishReasonError {
		return surfacedialog.FilePreviewData{}, false
	}
	finishPart := a.message.FinishPart()
	if finishPart == nil {
		return surfacedialog.FilePreviewData{}, false
	}

	var out strings.Builder
	if message := strings.TrimSpace(finishPart.Message); message != "" {
		out.WriteString(message)
		out.WriteString("\n")
	}
	if details := strings.TrimSpace(finishPart.Details); details != "" {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(details)
		out.WriteString("\n")
	}
	if content := strings.TrimSpace(a.message.Content().Text); content != "" {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString("Message content:\n")
		out.WriteString(content)
		out.WriteString("\n")
	}
	preview := strings.TrimSpace(out.String())
	if preview == "" {
		preview = "Assistant failed without error details."
	}
	return surfacedialog.FilePreviewData{
		Title:   "Assistant Error",
		Content: preview,
	}, true
}

func safeTextRenderWidth(width int) int {
	if width < minTextRenderWidth {
		return minTextRenderWidth
	}
	return width
}
