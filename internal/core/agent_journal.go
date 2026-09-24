package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// coreJournal persists engine writes through the store.
type coreJournal struct {
	c *Core
}

func (j coreJournal) Load(ctx context.Context, sessionID string) (agent.Window, error) {
	messages, err := j.c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return agent.Window{}, err
	}
	return agent.Window{Messages: messages}, nil
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
	return j.c.saveRunCheckpoint(ctx, state.RunID, RunCheckpointPhase(state.Phase), state.ToolCallID, state.ToolName)
}

func (j coreJournal) RecordStep(ctx context.Context, step agent.Step) error {
	j.c.recordRunStep(ctx, step)
	return nil
}
