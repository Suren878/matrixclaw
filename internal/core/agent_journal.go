package core

import (
	"context"
	"errors"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// coreJournal persists engine writes through the store.
type coreJournal struct {
	c *Core
}

func (j coreJournal) Load(ctx context.Context, sessionID string) (agent.Window, error) {
	return j.c.contextWindow(ctx, sessionID)
}

// contextWindow is what the model sees of a session: its newest context boundary
// and the messages after what the boundary covers.
func (c *Core) contextWindow(ctx context.Context, sessionID string) (agent.Window, error) {
	boundary, err := c.store.LatestCompaction(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		messages, err := c.store.ListMessages(ctx, sessionID, 0)
		return agent.Window{Messages: messages}, err
	}
	if err != nil {
		return agent.Window{}, err
	}
	messages, err := c.store.ListMessagesAfter(ctx, sessionID, boundary.Compaction.CoversThroughSeq, 0)
	if err != nil {
		return agent.Window{}, err
	}
	window := agent.Window{Boundary: &boundary, Messages: make([]transcript.Message, 0, len(messages))}
	for _, message := range messages {
		if message.Compaction == nil {
			window.Messages = append(window.Messages, message)
		}
	}
	return window, nil
}

func (j coreJournal) Append(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := j.c.store.AppendMessage(ctx, message)
	if err != nil {
		return 0, err
	}
	_ = j.c.touchSubagentTaskActivity(ctx, message.RunID, message.UpdatedAt)
	return seq, nil
}

func (j coreJournal) BeginStreaming(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := j.c.saveMessageProgress(ctx, message)
	if err != nil {
		return 0, err
	}
	_ = j.c.touchSubagentTaskActivity(ctx, message.RunID, message.UpdatedAt)
	return seq, nil
}

func (j coreJournal) Stream(ctx context.Context, message transcript.Message) error {
	if err := j.c.updateMessageProgress(ctx, message); err != nil {
		return err
	}
	_ = j.c.touchSubagentTaskActivity(ctx, message.RunID, message.UpdatedAt)
	return nil
}

func (j coreJournal) FinishStreaming(ctx context.Context, message transcript.Message) error {
	if err := j.c.store.UpdateMessage(ctx, message); err != nil {
		return err
	}
	_ = j.c.touchSubagentTaskActivity(ctx, message.RunID, message.UpdatedAt)
	return nil
}

func (j coreJournal) Checkpoint(ctx context.Context, state agent.State) error {
	return j.c.saveEngineCheckpoint(ctx, state)
}

func (j coreJournal) RecordStep(ctx context.Context, step agent.Step) error {
	j.c.recordRunStep(ctx, step)
	return nil
}
