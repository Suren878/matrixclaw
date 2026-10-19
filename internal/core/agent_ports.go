package core

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/events"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// coreSink publishes engine events on the core event bus.
type coreSink struct {
	c *Core
}

func (s coreSink) Emit(event agent.Event) {
	switch event.Kind {
	case agent.EventMessageCreated:
		s.c.publishEvent(events.Event{Type: EventMessageCreated, SessionID: event.SessionID, RunID: event.RunID, Payload: event.Message})
	case agent.EventMessageUpdated:
		s.c.publishEvent(events.Event{Type: EventMessageUpdated, SessionID: event.SessionID, RunID: event.RunID, Payload: event.Message})
	case agent.EventToolRequested:
		s.c.publishToolUpdate(event.SessionID, event.RunID, ToolUpdate{
			ToolCallID: event.ToolCallID,
			ToolName:   event.ToolName,
			State:      ToolLifecycleRequested,
			RunID:      event.RunID,
			SessionID:  event.SessionID,
		})
	case agent.EventToolFinished:
		prepared := preparedToolCall{SessionID: event.SessionID, RunID: event.RunID, ToolName: event.ToolName, ToolCallID: event.ToolCallID}
		s.c.publishFinishedToolUpdate(prepared, event.ResultMessageID, event.Result)
	case agent.EventContextMeasured:
		s.c.publishContextUsage(ContextUsage{SessionID: event.SessionID, TokenEstimate: event.ContextTokens, WindowTokens: event.WindowTokens}, event.RunID)
	}
}

// publishContextUsage announces a session's context size unless it is the one
// announced last.
func (c *Core) publishContextUsage(usage ContextUsage, runID string) {
	if last, ok := c.contextUsage.Swap(usage.SessionID, usage); ok && last == usage {
		return
	}
	c.publishEvent(events.Event{Type: EventContextUpdated, SessionID: usage.SessionID, RunID: runID, Payload: usage})
}

// coreJournal persists engine writes through the store and tells the run's
// tool batch of each checkpoint.
type coreJournal struct {
	c     *Core
	batch *toolBatch
}

func (j coreJournal) Load(ctx context.Context, sessionID string) (agent.Window, error) {
	return j.c.contextWindow(ctx, sessionID)
}

// contextWindow is what the model sees of a session: its newest context boundary
// and the messages after what the boundary covers. An unreadable boundary is
// logged once and ignored.
func (c *Core) contextWindow(ctx context.Context, sessionID string) (agent.Window, error) {
	boundary, err := c.store.LatestCompaction(ctx, sessionID)
	if err == nil && boundary.Compaction == nil {
		if _, logged := c.badBoundaries.LoadOrStore(boundary.ID, true); !logged {
			log.Printf("core: context boundary %q of session %q is unreadable; using the whole history", boundary.ID, sessionID)
		}
		err = ErrNotFound
	}
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

// Append writes a new message; one whose ID another session's message has
// (a provider reusing a tool call ID) fails with a clear error.
func (j coreJournal) Append(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := j.c.store.AppendMessage(ctx, message)
	if err == nil {
		return seq, nil
	}
	if existing, getErr := j.c.store.GetMessage(ctx, message.ID); getErr == nil && existing.SessionID != message.SessionID {
		return 0, fmt.Errorf("tool call id %q already belongs to another session", message.ID)
	}
	return 0, err
}

func (j coreJournal) BeginStreaming(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := j.c.store.SaveMessageProgress(ctx, message)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

func (j coreJournal) Stream(ctx context.Context, message transcript.Message) error {
	if err := j.c.store.UpdateMessageProgress(ctx, message); err != nil {
		return err
	}
	return nil
}

func (j coreJournal) FinishStreaming(ctx context.Context, message transcript.Message) error {
	if err := j.c.store.UpdateMessage(ctx, message); err != nil {
		return err
	}
	return nil
}

func (j coreJournal) Checkpoint(ctx context.Context, state agent.State) error {
	j.batch.checkpointed(state.Phase)
	return j.c.saveEngineCheckpoint(ctx, state)
}

func (j coreJournal) RecordStep(ctx context.Context, step agent.Step) {
	j.c.recordRunStep(ctx, step)
}
