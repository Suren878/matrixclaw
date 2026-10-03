package runtime

import (
	surfacechat "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacemodel "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/model"
)

// keptRows are the chat rows of the last sync by ID.
type keptRows map[string]chatRow

// reconcile reuses the kept row for every row whose signature did not change,
// so its render cache survives; a changed row keeps the old one's expansion.
func (kept keptRows) reconcile(rows []chatRow) []surfacechat.MessageItem {
	items := make([]surfacechat.MessageItem, len(rows))
	next := make(keptRows, len(rows))
	for i, row := range rows {
		id := row.item.ID()
		if old, ok := kept[id]; ok {
			if old.sig == row.sig {
				row = old
			} else if was, ok := old.item.(surfacechat.Expandable); ok && was.Expanded() {
				if now, ok := row.item.(surfacechat.Expandable); ok {
					now.ToggleExpanded()
				}
			}
		}
		next[id] = row
		items[i] = row.item
	}
	clear(kept)
	for id, row := range next {
		kept[id] = row
	}
	return items
}

// syncChat brings the chat rows up to date with the read model, keeping the
// selection and, unless the chat follows new output, the scroll position.
func (m *appModel) syncChat() {
	if m.read == nil {
		return
	}
	if m.chat == nil {
		m.chat = surfacemodel.NewChat(&surfacecommon.Common{Styles: &m.styles})
		if m.input.focus == appFocusChat {
			m.chat.Focus()
		}
	}
	follow := m.chat.Follow() || m.chat.Len() == 0
	selectedID := m.chat.SelectedMessageID()
	m.chat.SetMessages(m.rows.reconcile(buildChatRows(&m.styles, m.read, m.notes))...)
	if follow {
		m.chat.SelectLast()
		m.chat.ScrollToBottom()
	} else if selectedID != "" {
		_ = m.chat.SetSelectedByID(selectedID)
	}
	m.syncPromptHistory()
	m.pruneSuppressedApprovals()
}
