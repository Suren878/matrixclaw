package runtime

import (
	"slices"
	"strings"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
)

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
	snapshot := m.read.Snapshot()
	if len(m.transientMessages) > 0 {
		snapshot.Messages = append(append([]surfacemessage.Message(nil), snapshot.Messages...), m.transientMessages...)
		slices.SortStableFunc(snapshot.Messages, func(a surfacemessage.Message, b surfacemessage.Message) int {
			if a.CreatedAt < b.CreatedAt {
				return -1
			}
			if a.CreatedAt > b.CreatedAt {
				return 1
			}
			return 0
		})
	}
	chatModel := buildChatModel(&m.styles, snapshot)
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
	if m.focus == appFocusEditor || m.focus == appFocusPlan {
		m.chat.Blur()
	} else {
		m.chat.Focus()
	}
	m.pruneSuppressedApprovals()
}
