package chat

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

type UserMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	attachments *attachmentRenderer
	message     *surfacemessage.Message
	sty         *surfacestyles.Styles
}

func NewUserMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message) MessageItem {
	return &UserMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		attachments:              newAttachmentRenderer(sty),
		message:                  message,
		sty:                      sty,
	}
}

func (m *UserMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)

	content, height, ok := m.getCachedRender(cappedWidth)
	if ok {
		return m.renderHighlighted(content, cappedWidth, height)
	}

	msgContent := strings.TrimSpace(m.message.Content().Text)
	content = common.RenderMessageText(msgContent, m.sty, cappedWidth)

	if len(m.message.BinaryContent()) > 0 {
		attachmentsStr := m.renderAttachments(cappedWidth)
		if content == "" {
			content = attachmentsStr
		} else {
			content = strings.Join([]string{content, "", attachmentsStr}, "\n")
		}
	}

	height = lipgloss.Height(content)
	m.setCachedRender(content, cappedWidth, height)
	return m.renderHighlighted(content, cappedWidth, height)
}

func (m *UserMessageItem) Render(width int) string {
	return renderUserMessageLines(m.sty, m.RawRender(width), m.focused, width)
}

func (m *UserMessageItem) ID() string {
	return m.message.ID
}

func (m *UserMessageItem) renderAttachments(width int) string {
	return m.attachments.Render(m.message.BinaryContent(), false, width)
}
