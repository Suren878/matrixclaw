package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func TestLiveEventDoneSchedulesReconnect(t *testing.T) {
	model := newApp(nil, nil)
	next, cmd := model.Update(liveEventMsg{done: true})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected reconnect command")
	}
}

func TestLiveEventIgnoresStaleStream(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.streamID = 2
	model.session = "session-1"
	model.read = viewmodel.NewReadModel(snapshotWithTexts(now, "first"))

	messagePayload, _ := json.Marshal(core.Message{
		ID:        "msg-2",
		SessionID: "session-1",
		Role:      core.MessageRoleAssistant,
		Content:   "stale",
		Parts:     core.NormalizeMessageParts("stale", nil),
		CreatedAt: now.Add(time.Second),
		UpdatedAt: now.Add(time.Second),
	})
	next, cmd := model.Update(liveEventMsg{
		streamID: 1,
		event: daemonclient.LiveEvent{
			Type:      core.EventMessageCreated,
			SessionID: "session-1",
			Payload:   messagePayload,
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no follow-up command for stale stream")
	}
	if got := len(model.read.Snapshot().Messages); got != 1 {
		t.Fatalf("len(messages) = %d, want 1", got)
	}
}

func TestLiveEventIgnoresDifferentSession(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.streamID = 1
	model.session = "session-1"
	model.read = viewmodel.NewReadModel(snapshotWithTexts(now, "first"))

	messagePayload, _ := json.Marshal(core.Message{
		ID:        "msg-2",
		SessionID: "session-2",
		Role:      core.MessageRoleAssistant,
		Content:   "other",
		Parts:     core.NormalizeMessageParts("other", nil),
		CreatedAt: now.Add(time.Second),
		UpdatedAt: now.Add(time.Second),
	})
	next, cmd := model.Update(liveEventMsg{
		streamID: 1,
		event: daemonclient.LiveEvent{
			Type:      core.EventMessageCreated,
			SessionID: "session-2",
			Payload:   messagePayload,
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no follow-up command for different session event")
	}
	if got := len(model.read.Snapshot().Messages); got != 1 {
		t.Fatalf("len(messages) = %d, want 1", got)
	}
}

func TestRunUpdatedCompletedTriggersSnapshotReload(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, &Runtime{})
	model.streamID = 1
	model.session = "session-1"
	model.read = viewmodel.NewReadModel(snapshotWithTexts(now, "first"))

	runPayload, _ := json.Marshal(core.Run{
		ID:        "run-1",
		SessionID: "session-1",
		Status:    core.RunStatusCompleted,
		UpdatedAt: now,
	})
	next, cmd := model.Update(liveEventMsg{
		streamID: 1,
		event: daemonclient.LiveEvent{
			Type:      core.EventRunUpdated,
			SessionID: "session-1",
			RunID:     "run-1",
			Payload:   runPayload,
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected snapshot reload command")
	}
}

func TestRunIsActiveTreatsWaitingApprovalAsBusy(t *testing.T) {
	if !runIsActive(&core.Run{Status: core.RunStatusWaitingApproval}) {
		t.Fatal("expected waiting approval run to be active")
	}
}

func TestCurrentModelLabelPrefersSessionModelOverMessageHistory(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	snapshot := snapshotWithTexts(now, "old response")
	snapshot.Session = &core.Session{
		ID:         "session-1",
		ModelID:    "glm-4.7",
		ProviderID: "zai",
	}
	snapshot.Messages[0].Model = "gpt-5.4-mini"
	model.read = viewmodel.NewReadModel(snapshot)

	if got := model.currentModelLabel(); got != "glm-4.7" {
		t.Fatalf("currentModelLabel() = %q, want glm-4.7", got)
	}
}

func TestLoadInitialErrorWithExistingSessionSchedulesReconnect(t *testing.T) {
	model := newApp(context.Background(), &Runtime{})
	model.session = "session-1"

	next, cmd := model.Update(loadInitialMsg{
		err: fmt.Errorf("temporary outage"),
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected reconnect command")
	}
	if model.err == "" {
		t.Fatal("expected error message to be set")
	}
}

func TestApplySnapshotClearsStaleControlplaneError(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.err = "Session `default` now uses provider `gemini` with model `gemma-3`."

	model.applySnapshot(snapshotWithTexts(now, "ok"), true)

	if model.err != "" {
		t.Fatalf("err = %q, want cleared stale controlplane text", model.err)
	}
}

func TestApplySnapshotKeepsFailedRunError(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	snapshot := snapshotWithTexts(now, "failed")
	snapshot.Run = &core.Run{ID: "run-1", SessionID: "session-1", Status: core.RunStatusFailed, Error: "provider failed"}

	model.applySnapshot(snapshot, true)

	if model.err != "provider failed" {
		t.Fatalf("err = %q, want failed run error", model.err)
	}
}
