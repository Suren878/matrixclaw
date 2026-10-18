package core

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
)

// RunCheckpoint marks a run that passed an execution boundary; a run that has
// one when the daemon starts is recovered.
type RunCheckpoint struct {
	RunID string `json:"run_id"`
	// Batch is set in the engine's tool_batch phase.
	Batch *agent.ToolBatch `json:"tool_batch,omitempty"`
	// RecoveryCount counts the daemon restarts the run was recovered from
	// since it last finished a step.
	RecoveryCount int             `json:"recovery_count,omitempty"`
	EngineState   json.RawMessage `json:"engine_state,omitempty"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// RunCheckpointStore keeps the execution boundaries crash recovery resumes from.
type RunCheckpointStore interface {
	SaveRunCheckpoint(ctx context.Context, checkpoint RunCheckpoint) error
	GetRunCheckpoint(ctx context.Context, runID string) (RunCheckpoint, error)
	DeleteRunCheckpoint(ctx context.Context, runID string) error
}

// saveEngineCheckpoint stores the engine's batch together with its run
// state; while a native run is active only its engine calls it. A model
// phase after a tool batch means a step finished, which resets the recovery
// count: only restarts in a row without progress stop a run.
func (c *Core) saveEngineCheckpoint(ctx context.Context, state agent.State) error {
	engine, err := json.Marshal(state.RunState)
	if err != nil {
		return err
	}
	return c.updateRunCheckpoint(ctx, state.RunID, func(checkpoint *RunCheckpoint) {
		if state.Phase == agent.PhaseModel && checkpoint.Batch != nil {
			checkpoint.RecoveryCount = 0
		}
		checkpoint.Batch = state.Batch
		checkpoint.EngineState = engine
	})
}

// updateRunCheckpoint applies update to the run's checkpoint and keeps every field
// update leaves alone, such as the recovery count or the engine state.
func (c *Core) updateRunCheckpoint(ctx context.Context, runID string, update func(*RunCheckpoint)) error {
	if runID == "" {
		return nil
	}
	checkpoint, err := c.store.GetRunCheckpoint(ctx, runID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	checkpoint.RunID = runID
	update(&checkpoint)
	checkpoint.UpdatedAt = c.now().UTC()
	return c.store.SaveRunCheckpoint(ctx, checkpoint)
}

// resumeState reads the engine state of the run's checkpoint; a checkpoint
// without a readable one starts the budget from zero.
func (c *Core) resumeState(ctx context.Context, runID string) (agent.RunState, error) {
	checkpoint, ok, err := c.runCheckpoint(ctx, runID)
	if err != nil || !ok || len(checkpoint.EngineState) == 0 {
		return agent.RunState{}, err
	}
	var state agent.RunState
	if err := json.Unmarshal(checkpoint.EngineState, &state); err != nil {
		log.Printf("core: run %q has unreadable engine state, starting from zero: %v", runID, err)
		return agent.RunState{}, nil
	}
	return state, nil
}

// carriedState is the state the session's previous run left for the
// next one; unreadable ones start from zero.
func (c *Core) carriedState(ctx context.Context, sessionID string) (agent.RunState, error) {
	stored, err := c.store.GetSessionEngineState(ctx, sessionID)
	if err != nil || len(stored) == 0 {
		return agent.RunState{}, err
	}
	var state agent.RunState
	if err := json.Unmarshal(stored, &state); err != nil {
		log.Printf("core: session %q has unreadable carried state, starting from zero: %v", sessionID, err)
		return agent.RunState{}, nil
	}
	return state, nil
}

// carryState keeps what a run's state carries over to the session's next run.
func (c *Core) carryState(ctx context.Context, sessionID string, state agent.RunState) {
	carried := state.Carried()
	if carried == (agent.RunState{}) {
		return
	}
	stored, err := json.Marshal(carried)
	if err == nil {
		err = c.store.SaveSessionEngineState(ctx, sessionID, stored, c.now().UTC())
	}
	if err != nil {
		log.Printf("core: keep carried state of session %q: %v", sessionID, err)
	}
}

// markRunRecovery counts one more recovery of the run and returns its
// checkpoint as it was, with the new count; the batch it names is dropped.
func (c *Core) markRunRecovery(ctx context.Context, runID string) (RunCheckpoint, error) {
	checkpoint, err := c.store.GetRunCheckpoint(ctx, runID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return RunCheckpoint{}, err
	}
	batch := checkpoint.Batch
	checkpoint.RunID = runID
	checkpoint.Batch = nil
	checkpoint.RecoveryCount++
	checkpoint.UpdatedAt = c.now().UTC()
	if err := c.store.SaveRunCheckpoint(ctx, checkpoint); err != nil {
		return RunCheckpoint{}, err
	}
	checkpoint.Batch = batch
	return checkpoint, nil
}

func (c *Core) runCheckpoint(ctx context.Context, runID string) (RunCheckpoint, bool, error) {
	checkpoint, err := c.store.GetRunCheckpoint(ctx, runID)
	if errors.Is(err, ErrNotFound) {
		return RunCheckpoint{}, false, nil
	}
	if err != nil {
		return RunCheckpoint{}, false, err
	}
	return checkpoint, true, nil
}

// runRecoveryNotice tells a recovered run that it was interrupted.
const runRecoveryNotice = "Recovery notice: the MatrixClaw daemon restarted while this run was active. Continue from the durable conversation and workspace state. Do not repeat a mutating action merely because its previous result is missing. Inspect the current state first, and only retry a mutation when an explicit recovery approval or clear evidence shows it is still required."
