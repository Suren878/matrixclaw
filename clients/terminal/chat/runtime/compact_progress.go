package runtime

import (
	"strings"
	"time"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
)

const (
	compactProgressMessageID = "local:context-compact"
	compactProgressText      = "🧠 Summarizing context..."
	compactCompleteText      = "✅ Summarizing complete."
	compactFailedPrefix      = "❌ Summarizing failed"
)

func isContextCompactCommand(command string) bool {
	return strings.EqualFold(strings.TrimSpace(command), "/context compact confirm")
}

func (m *appModel) startContextCompactProgress() {
	m.err = ""
	m.upsertTransientMessage(newCompactTransientMessage(compactProgressText))
	m.rebuildChat()
}

func (m *appModel) completeContextCompactProgress(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		text = compactCompleteText
	}
	m.err = ""
	m.upsertTransientMessage(newCompactTransientMessage(text))
	m.rebuildChat()
}

func (m *appModel) failContextCompactProgress(err error) {
	text := compactFailedPrefix + "."
	details := ""
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		details = strings.TrimSpace(err.Error())
		text = compactFailedPrefix + ": " + details
	}
	m.err = ""
	message := newCompactTransientMessage(text)
	message.AddFinish(surfacemessage.FinishReasonError, compactFailedPrefix, details)
	m.upsertTransientMessage(message)
	m.rebuildChat()
}

func (m *appModel) clearContextCompactProgress() {
	m.removeTransientMessage(compactProgressMessageID)
}

func newCompactTransientMessage(text string) surfacemessage.Message {
	now := time.Now().Unix()
	return surfacemessage.Message{
		ID:               compactProgressMessageID,
		Role:             surfacemessage.System,
		Parts:            []surfacemessage.ContentPart{surfacemessage.TextContent{Text: text}},
		CreatedAt:        now,
		UpdatedAt:        now,
		IsSummaryMessage: true,
	}
}
