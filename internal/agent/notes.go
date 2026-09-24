package agent

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// appendEngineMessage journals a note from the engine: the model reads it as user
// text and clients show it as a system note. A note that repeats the last message
// is skipped, so a restarted run does not write it twice.
func (r *run) appendEngineMessage(ctx context.Context, text string) error {
	if messages := r.history.all(); len(messages) > 0 {
		if last := messages[len(messages)-1]; last.Origin == transcript.OriginEngine && last.Content == text {
			return nil
		}
	}
	now := r.Now()
	return r.history.append(ctx, transcript.Message{
		ID:        r.NewID("msg"),
		SessionID: r.task.SessionID,
		RunID:     r.task.RunID,
		Role:      transcript.MessageRoleSystem,
		Origin:    transcript.OriginEngine,
		Content:   text,
		Parts:     transcript.NormalizeMessageParts(text, nil),
		CreatedAt: now,
		UpdatedAt: now,
	})
}
