package runtime

import (
	"strings"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacelist "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/list"
	surfacemodel "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/model"
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
	chatModel := surfacemodel.NewChat(&surfacecommon.Common{Styles: &m.styles})
	chatModel.SetMessages(buildChatItems(&m.styles, m.read, m.transientMessages)...)
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
	if m.focus == appFocusEditor {
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
