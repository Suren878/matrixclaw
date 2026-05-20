package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/clientruntime"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (w *Worker) monitorRunEvents(ctx context.Context, target chatTarget, sessionID string, runID string, state *runDeliveryState, afterID *uint64) (bool, error) {
	daemon := w.daemon(target.externalKey)
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, errs, err := daemon.SubscribeEvents(streamCtx, sessionID, currentEventID(afterID))
	if err != nil {
		return false, err
	}

	liveState, done, err := w.renderInitialRunSnapshot(ctx, target, daemon, sessionID, runID, state)
	if done || err != nil {
		return done, err
	}

	ticker := time.NewTicker(w.config.StreamFlushInterval)
	defer ticker.Stop()

	assistantDirty := false
	flush := func() error {
		if !assistantDirty {
			return nil
		}
		if err := w.renderAssistantUpdates(ctx, target, liveState.Snapshot().Messages, runID, state); err != nil {
			return err
		}
		assistantDirty = false
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return true, nil
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				return false, err
			}
		case event, ok := <-events:
			if !ok {
				if err := flush(); err != nil {
					return false, err
				}
				return false, fmt.Errorf("event stream closed")
			}
			recordEventID(afterID, event.ID)
			outcome, err := w.handleRunEvent(ctx, target, daemon, runID, state, liveState, event, flush)
			if outcome.assistantDirty {
				assistantDirty = true
			}
			if outcome.done || err != nil {
				return outcome.done, err
			}
		case <-ticker.C:
			if err := flush(); err != nil {
				log.Printf("telegram: flush assistant updates for run %s: %v", runID, err)
			}
		}
	}
}

func (w *Worker) renderInitialRunSnapshot(ctx context.Context, target chatTarget, daemon *daemonclient.Client, sessionID string, runID string, state *runDeliveryState) (*clientruntime.State, bool, error) {
	snapshot, err := daemon.LoadSnapshot(ctx)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(snapshot.SessionID) != "" && strings.TrimSpace(snapshot.SessionID) != strings.TrimSpace(sessionID) {
		w.finishMonitor(target.externalKey, runID)
		return nil, true, nil
	}
	liveState := clientruntime.NewState(snapshot)
	if err := w.renderVoiceToolResultUpdates(ctx, target, snapshot.Messages, runID, state); err != nil {
		return nil, false, err
	}
	if err := w.renderAssistantUpdates(ctx, target, snapshot.Messages, runID, state); err != nil {
		return nil, false, err
	}
	if err := w.renderApprovalUpdates(ctx, target, snapshot.Approvals, runID, state); err != nil {
		return nil, false, err
	}
	run := snapshot.Run
	if run == nil || strings.TrimSpace(run.ID) != strings.TrimSpace(runID) {
		loaded, loadErr := daemon.GetRun(ctx, runID)
		if loadErr == nil {
			run = &loaded
		}
	}
	if run == nil {
		return liveState, false, nil
	}
	done, err := w.handleRunTerminalState(ctx, target, daemon, runID, state, *run, func() error { return nil })
	return liveState, done, err
}

type runEventOutcome struct {
	assistantDirty bool
	done           bool
}

func (w *Worker) handleRunEvent(ctx context.Context, target chatTarget, daemon *daemonclient.Client, runID string, state *runDeliveryState, liveState *clientruntime.State, event daemonclient.LiveEvent, flush func() error) (runEventOutcome, error) {
	if strings.TrimSpace(event.RunID) != "" && strings.TrimSpace(event.RunID) != strings.TrimSpace(runID) {
		return runEventOutcome{}, nil
	}
	if err := liveState.Apply(event); err != nil {
		return runEventOutcome{}, err
	}
	switch event.Type {
	case core.EventMessageCreated, core.EventMessageUpdated:
		message, err := event.DecodeMessage()
		if err != nil {
			return runEventOutcome{}, err
		}
		if message.Role == core.MessageRoleTool && strings.TrimSpace(message.RunID) == strings.TrimSpace(runID) {
			if err := w.renderVoiceToolResultUpdates(ctx, target, []core.Message{message}, runID, state); err != nil {
				return runEventOutcome{}, err
			}
			return runEventOutcome{}, nil
		}
		if message.Role == core.MessageRoleAssistant && strings.TrimSpace(message.RunID) == strings.TrimSpace(runID) {
			return runEventOutcome{assistantDirty: true}, nil
		}
	case core.EventApprovalRequest:
		request, err := event.DecodePermissionRequest()
		if err != nil {
			return runEventOutcome{}, err
		}
		approval := core.Approval{
			ID:          request.ID,
			SessionID:   request.SessionID,
			RunID:       event.RunID,
			ToolCallRef: request.ToolCallID,
			ToolName:    request.ToolName,
			Description: request.Description,
			Action:      request.Action,
			Params:      request.Params,
			Path:        request.Path,
			State:       core.ApprovalStatePending,
		}
		if err := w.renderApprovalUpdates(ctx, target, []core.Approval{approval}, runID, state); err != nil {
			return runEventOutcome{}, err
		}
	case core.EventRunUpdated:
		run, err := event.DecodeRun()
		if err != nil {
			return runEventOutcome{}, err
		}
		done, err := w.handleRunTerminalState(ctx, target, daemon, runID, state, run, flush)
		return runEventOutcome{done: done}, err
	}
	return runEventOutcome{}, nil
}

func (w *Worker) handleRunTerminalState(ctx context.Context, target chatTarget, daemon *daemonclient.Client, runID string, state *runDeliveryState, run core.Run, flush func() error) (bool, error) {
	if strings.TrimSpace(run.ID) != strings.TrimSpace(runID) {
		return false, nil
	}
	switch run.Status {
	case core.RunStatusWaitingApproval:
		return false, nil
	case core.RunStatusCompleted:
		if err := flush(); err != nil {
			return false, err
		}
		w.ackRunDelivery(ctx, daemon, state)
		w.finishMonitor(target.externalKey, runID)
		return true, nil
	case core.RunStatusFailed, core.RunStatusCanceled:
		if err := flush(); err != nil {
			return false, err
		}
		if !state.statusNotified && len(state.assistant) == 0 {
			state.statusNotified = true
			if err := w.sendText(ctx, target, renderRunStatus(run)); err != nil {
				return false, err
			}
		}
		w.ackRunDelivery(ctx, daemon, state)
		w.finishMonitor(target.externalKey, runID)
		return true, nil
	default:
		return false, nil
	}
}

func currentEventID(afterID *uint64) uint64 {
	if afterID == nil {
		return 0
	}
	return *afterID
}

func recordEventID(afterID *uint64, eventID uint64) {
	if afterID != nil && eventID > *afterID {
		*afterID = eventID
	}
}
