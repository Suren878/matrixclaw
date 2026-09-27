package agent

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

const contextNotePrefix = "Context update"

// syncContext sends the state the system prompt leaves out as a context note
// whenever it differs from the newest note the model still sees; reading that
// note from the history keeps a restarted run or a summary consistent.
func (r *run) syncContext(ctx context.Context) error {
	text := strings.TrimSpace(r.Prompts.Context(ctx))
	last, sent := r.lastContextNote()
	if !sent && text == "" {
		return nil
	}
	note := contextNoteText(text)
	if sent && last == note {
		return nil
	}
	return r.appendEngineMessage(ctx, transcript.OriginEngineModel, note)
}

// lastContextNote is the newest context note after the boundary.
func (r *run) lastContextNote() (string, bool) {
	_, messages := r.history.window()
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Origin == transcript.OriginEngineModel && strings.HasPrefix(message.Content, contextNotePrefix) {
			return message.Content, true
		}
	}
	return "", false
}

func contextNoteText(text string) string {
	if text == "" {
		return contextNotePrefix + ": nothing from the earlier context updates applies any more."
	}
	return contextNotePrefix + " (the current state; it replaces earlier context updates):\n\n" + text
}
