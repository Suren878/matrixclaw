package runtime

import (
	"strings"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
)

func (m *appModel) upsertTransientMessage(message surfacemessage.Message) {
	id := strings.TrimSpace(message.ID)
	if id == "" {
		return
	}
	for i := range m.transientMessages {
		if m.transientMessages[i].ID == id {
			m.transientMessages[i] = message
			return
		}
	}
	m.transientMessages = append(m.transientMessages, message)
}

func (m *appModel) removeTransientMessage(id string) {
	id = strings.TrimSpace(id)
	if id == "" || len(m.transientMessages) == 0 {
		return
	}
	next := m.transientMessages[:0]
	for _, message := range m.transientMessages {
		if message.ID != id {
			next = append(next, message)
		}
	}
	m.transientMessages = next
}
