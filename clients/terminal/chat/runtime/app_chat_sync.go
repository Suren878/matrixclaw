package runtime

import (
	"slices"
	"strings"

	surfacelist "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/list"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
)

func (m *appModel) rebuildChat() {
	if m.read == nil {
		return
	}
	selectedID := ""
	follow := true
	var viewport surfacemodelViewportSnapshot
	if m.chat != nil {
		selectedID = m.chat.SelectedMessageID()
		follow = m.chat.Follow()
		viewport = surfacemodelViewportSnapshot{snapshot: m.chat.SnapshotViewport(), ok: true}
	}
	snapshot := m.currentSnapshot()
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
	m.chat = chatModel
	m.resizeChat()
	if follow || !viewport.ok {
		m.chat.SelectLast()
		m.chat.ScrollToBottom()
	} else {
		if strings.TrimSpace(selectedID) != "" {
			_ = m.chat.SetSelectedByID(selectedID)
		}
		m.chat.RestoreViewport(viewport.snapshot)
	}
	m.syncPromptHistory()
	if m.focus == appFocusEditor || m.focus == appFocusPlan {
		m.chat.Blur()
	} else {
		m.chat.Focus()
	}
	m.pruneSuppressedApprovals()
}

type surfacemodelViewportSnapshot struct {
	snapshot surfacelist.ViewportSnapshot
	ok       bool
}
