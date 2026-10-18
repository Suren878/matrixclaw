package core

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ClearContext starts the session's model context afresh with a boundary that
// covers every message so far and an empty todo list; the transcript keeps the
// messages, but not the tool outputs kept in files for them.
func (c *Core) ClearContext(ctx context.Context, sessionID string) (transcript.Message, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return transcript.Message{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return transcript.Message{}, err
	}
	if active, err := c.sessionRunActive(ctx, sessionID); err != nil {
		return transcript.Message{}, err
	} else if active {
		return transcript.Message{}, fmt.Errorf("%w: finish or cancel the current run before clearing context", ErrRunActive)
	}
	boundary, err := c.sessionEngine().ClearContext(ctx, sessionID)
	if err != nil {
		return transcript.Message{}, err
	}
	if err := c.pruneToolOutputs(ctx, sessionID, boundary.Compaction.CoversThroughSeq); err != nil {
		log.Printf("core: remove cleared tool outputs of session %q: %v", sessionID, err)
	}
	if _, err := c.saveSessionTodo(ctx, todo.List{SessionID: sessionID}); err != nil {
		return transcript.Message{}, err
	}
	return boundary, nil
}
