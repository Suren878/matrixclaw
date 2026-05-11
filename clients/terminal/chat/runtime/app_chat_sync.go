package runtime

import "strings"

func (m *appModel) rebuildChat() {
	if m.read == nil {
		return
	}
	selectedID := ""
	follow := true
	if m.chat != nil {
		selectedID = m.chat.SelectedMessageID()
		follow = m.chat.Follow()
	}
	chatModel := buildChatModel(&m.styles, m.read.Snapshot())
	chatModel.Focus()
	if follow || strings.TrimSpace(selectedID) == "" {
		chatModel.SelectLast()
		chatModel.ScrollToBottom()
	} else if chatModel.SetSelectedByID(selectedID) {
		chatModel.ScrollToSelected()
	} else {
		chatModel.SelectLast()
		chatModel.ScrollToBottom()
	}
	m.chat = chatModel
	m.resizeChat()
	m.syncPromptHistory()
	if m.focus == appFocusEditor {
		m.chat.Blur()
	} else {
		m.chat.Focus()
	}
	m.pruneSuppressedApprovals()
}
