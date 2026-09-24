package agent

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// appendEngineMessage journals a note from the engine: the model reads it as user
// text; origin says whether clients show it. A note that repeats the last message
// is skipped, so a restarted run does not write it twice; a reply the restart
// cut after the note does not count as the last message.
func (r *run) appendEngineMessage(ctx context.Context, origin transcript.Origin, text string) error {
	messages := r.history.all()
	for i := len(messages) - 1; i >= 0; i-- {
		if transcript.HasFinishReason(messages[i], transcript.FinishReasonDaemonRestart) {
			continue
		}
		if messages[i].Origin == origin && messages[i].Content == text {
			return nil
		}
		break
	}
	now := r.Now()
	return r.history.append(ctx, transcript.Message{
		ID:        r.NewID("msg"),
		SessionID: r.task.SessionID,
		RunID:     r.task.RunID,
		Role:      transcript.MessageRoleSystem,
		Origin:    origin,
		Content:   text,
		Parts:     transcript.NormalizeMessageParts(text, nil),
		CreatedAt: now,
		UpdatedAt: now,
	})
}
