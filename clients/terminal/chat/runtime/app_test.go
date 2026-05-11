package runtime

import (
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func snapshotWithTexts(now time.Time, texts ...string) core.ClientSnapshot {
	out := core.ClientSnapshot{
		SessionID: "session-1",
	}
	for i, text := range texts {
		id := i + 1
		out.Messages = append(out.Messages, core.Message{
			ID:        "msg-" + itoa(id),
			SessionID: "session-1",
			Role:      core.MessageRoleAssistant,
			Content:   text,
			Parts: []core.MessagePart{
				{
					Kind: core.MessagePartKindText,
					Text: &core.TextPart{Text: text},
				},
			},
			CreatedAt: now.Add(time.Duration(id) * time.Second),
			UpdatedAt: now.Add(time.Duration(id) * time.Second),
		})
	}
	return out
}

func itoa(v int) string {
	switch v {
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	case 4:
		return "4"
	case 5:
		return "5"
	default:
		return "0"
	}
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
