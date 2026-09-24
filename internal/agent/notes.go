package agent

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// appendEngineMessage journals a note from the engine: the model reads it as user
// text and clients show it as a system note.
func (r *run) appendEngineMessage(ctx context.Context, text string) error {
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
