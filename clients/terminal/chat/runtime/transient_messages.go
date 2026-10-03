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
	for i := range m.notes {
		if m.notes[i].ID == id {
			m.notes[i] = message
			return
		}
	}
	m.notes = append(m.notes, message)
}

func (m *appModel) removeTransientMessage(id string) {
	id = strings.TrimSpace(id)
	if id == "" || len(m.notes) == 0 {
		return
	}
	next := m.notes[:0]
	for _, message := range m.notes {
		if message.ID != id {
			next = append(next, message)
		}
	}
	m.notes = next
}
