package runtime

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
)

func (m *appModel) restoreEditorDraft(content string, attachments []surfaceeditor.Attachment) {
	if strings.TrimSpace(content) != "" {
		m.input.Editor().SetValue(content)
		m.input.Editor().MoveToEnd()
	}
	for _, attachment := range attachments {
		_ = m.input.Editor().UpdateAttachments(attachment)
	}
}

func (m *appModel) attachFilesFromEditorValue() error {
	content := strings.TrimSpace(m.input.Editor().Value())
	if content == "" {
		return fmt.Errorf("paste or type a file path first")
	}

	paths := parsePastedFiles(content)
	if len(paths) > 0 {
		attachments := make([]surfaceeditor.Attachment, 0, len(paths))
		for _, path := range paths {
			attachment, err := attachmentFromPath(path)
			if err != nil {
				return err
			}
			attachments = append(attachments, attachment)
		}
		if len(attachments) > 0 {
			for _, attachment := range attachments {
				_ = m.input.Editor().UpdateAttachments(attachment)
			}
			m.input.Editor().Reset()
			return nil
		}
	}

	return fmt.Errorf("paste or type one or more valid file paths first")
}

func (m *appModel) sendMessageCmd(content string, attachments []surfaceeditor.Attachment) tea.Cmd {
	if m.rt == nil {
		return func() tea.Msg {
			return sendMessageResultMsg{
				content:     content,
				attachments: attachments,
				err:         fmt.Errorf("terminal runtime is not configured"),
			}
		}
	}
	sessionID := strings.TrimSpace(m.session)
	return func() tea.Msg {
		result, err := m.rt.sendMessage(m.ctx, sessionID, content, attachments...)
		return sendMessageResultMsg{
			content:     content,
			attachments: attachments,
			result:      result,
			err:         err,
		}
	}
}

func (m *appModel) createSessionCmd() tea.Cmd {
	if m.rt == nil {
		return nil
	}
	return func() tea.Msg {
		snapshot, err := m.rt.createAndLoadSession(m.ctx, "Session")
		return loadInitialMsg{snapshot: snapshot, err: err}
	}
}
