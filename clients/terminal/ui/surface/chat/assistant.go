package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	termmarkdown "github.com/MichaelMure/go-term-markdown"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/anim"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const maxCollapsedThinkingHeight = 10

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

	message           *surfacemessage.Message
	sty               *surfacestyles.Styles
	anim              *anim.Anim
	thinkingExpanded  bool
	thinkingBoxHeight int
}

func NewAssistantMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message) MessageItem {
	a := &AssistantMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		message:                  message,
		sty:                      sty,
	}

	a.anim = anim.New(anim.Settings{
		ID:          a.ID(),
		Size:        15,
		GradColorA:  sty.Primary,
		GradColorB:  sty.Secondary,
		LabelColor:  sty.FgBase,
		CycleColors: true,
	})
	return a
}

func (a *AssistantMessageItem) StartAnimation() tea.Cmd {
	if !a.isSpinning() {
		return nil
	}
	return a.anim.Start()
}

func (a *AssistantMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if !a.isSpinning() {
		return nil
	}
	return a.anim.Animate(msg)
}

func (a *AssistantMessageItem) ID() string {
	return a.message.ID
}

func (a *AssistantMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)

	var spinner string
	if a.isSpinning() {
		spinner = a.renderSpinning()
	}

	content, height, ok := a.getCachedRender(cappedWidth)
	if !ok {
		content = a.renderMessageContent(cappedWidth)
		height = lipgloss.Height(content)
		a.setCachedRender(content, cappedWidth, height)
	}

	highlightedContent := a.renderHighlighted(content, cappedWidth, height)
	if spinner != "" {
		if highlightedContent != "" {
			highlightedContent += "\n\n"
		}
		return highlightedContent + spinner
	}
	return highlightedContent
}

func (a *AssistantMessageItem) Render(width int) string {
	return renderUnifiedMessageLines(a.sty, a.RawRender(width), a.focused, a.sty.Chat.Message.AssistantMarker)
}

func (a *AssistantMessageItem) renderMessageContent(width int) string {
	var messageParts []string
	thinking := strings.TrimSpace(a.message.ReasoningContent().Thinking)
	content := strings.TrimSpace(a.message.Content().Text)

	if thinking != "" {
		messageParts = append(messageParts, a.renderThinking(a.message.ReasoningContent().Thinking, width))
	}

	if content != "" {
		if thinking != "" {
			messageParts = append(messageParts, "")
		}
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

func (a *AssistantMessageItem) renderThinking(thinking string, width int) string {
	width = safeTextRenderWidth(width)
	renderer := common.PlainMarkdownRenderer(a.sty, width)
	rendered, err := renderer.Render(thinking)
	if err != nil {
		rendered = thinking
	}
	rendered = strings.TrimSpace(rendered)

	lines := strings.Split(rendered, "\n")
	totalLines := len(lines)
	isTruncated := totalLines > maxCollapsedThinkingHeight
	if !a.thinkingExpanded && isTruncated {
		lines = lines[totalLines-maxCollapsedThinkingHeight:]
		hint := a.sty.Chat.Message.ThinkingTruncationHint.Render(
			fmt.Sprintf(assistantMessageTruncateFormat, totalLines-maxCollapsedThinkingHeight),
		)
		lines = append([]string{hint, ""}, lines...)
	}

	thinkingStyle := a.sty.Chat.Message.ThinkingBox.Width(width)
	result := thinkingStyle.Render(strings.Join(lines, "\n"))
	a.thinkingBoxHeight = lipgloss.Height(result)

	var footer string
	if !a.message.IsThinking() || len(a.message.ToolCalls()) > 0 {
		duration := a.message.ThinkingDuration()
		if duration.String() != "0s" {
			footer = a.sty.Chat.Message.ThinkingFooterTitle.Render("Thought for ") +
				a.sty.Chat.Message.ThinkingFooterDuration.Render(duration.String())
		}
	}
	if footer != "" {
		result += "\n\n" + footer
	}
	return result
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

func (a *AssistantMessageItem) renderSpinning() string {
	if a.message.IsThinking() {
		return a.anim.RenderDots(":: Thinking")
	} else if a.message.IsSummaryMessage {
		return a.anim.RenderDots(":: Summarizing")
	}
	return a.anim.RenderDots(":: Working")
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

func (a *AssistantMessageItem) isSpinning() bool {
	isThinking := a.message.IsThinking()
	isFinished := a.message.IsFinished()
	hasContent := strings.TrimSpace(a.message.Content().Text) != ""
	hasToolCalls := len(a.message.ToolCalls()) > 0
	return (isThinking || !isFinished) && !hasContent && !hasToolCalls
}

func (a *AssistantMessageItem) SetMessage(message *surfacemessage.Message) tea.Cmd {
	wasSpinning := a.isSpinning()
	a.message = message
	a.clearCache()
	if !wasSpinning && a.isSpinning() {
		return a.StartAnimation()
	}
	return nil
}

func (a *AssistantMessageItem) ToggleExpanded() bool {
	a.thinkingExpanded = !a.thinkingExpanded
	a.clearCache()
	return a.thinkingExpanded
}
