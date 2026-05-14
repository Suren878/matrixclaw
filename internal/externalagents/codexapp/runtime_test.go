package codexapp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func TestRuntimeStartsSessionAndNormalizesTurnEvents(t *testing.T) {
	clientConn, serverConn := pipePair()
	defer clientConn.Close()
	defer serverConn.Close()

	go serveCodexAppProtocol(t, serverConn)

	runtime := NewRuntime(RuntimeOptions{
		Enabled: true,
		Client:  NewClient(clientConn),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session, err := runtime.StartSession(ctx, externalagents.StartSessionRequest{
		Model: "gpt-5.4",
		CWD:   "/tmp/project",
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if session.AgentID != AgentID || session.ExternalThreadID != "thread_1" {
		t.Fatalf("unexpected session: %+v", session)
	}

	events, err := runtime.Send(ctx, session, externalagents.Input{Text: "hello"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	var got []externalagents.Event
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 2 {
		t.Fatalf("event count = %d, want 2: %+v", len(got), got)
	}
	if got[0].Kind != externalagents.EventMessageDelta || got[0].Text != "pong" {
		t.Fatalf("first event = %+v, want message delta pong", got[0])
	}
	if got[1].Kind != externalagents.EventTurnCompleted {
		t.Fatalf("second event = %+v, want turn completed", got[1])
	}
}

func TestRuntimeStartsSessionWithUsableSandboxDefaults(t *testing.T) {
	clientConn, serverConn := pipePair()
	defer clientConn.Close()
	defer serverConn.Close()

	gotParams := make(chan ThreadStartParams, 1)
	go func() {
		dec := json.NewDecoder(serverConn)
		enc := json.NewEncoder(serverConn)
		for {
			var req wireRequest
			if err := dec.Decode(&req); err != nil {
				return
			}
			switch req.Method {
			case "initialize":
				writeResult(t, enc, req.ID, InitializeResponse{
					UserAgent:      "codex-test/0",
					CodexHome:      "/tmp/codex",
					PlatformFamily: "unix",
					PlatformOS:     "linux",
				})
			case "initialized":
			case "thread/start":
				var params ThreadStartParams
				if err := json.Unmarshal(req.Params, &params); err != nil {
					t.Errorf("decode thread/start params: %v", err)
					return
				}
				gotParams <- params
				writeResult(t, enc, req.ID, ThreadStartResponse{
					Thread: Thread{ID: "thread_1", SessionID: "session_1", CWD: "/tmp/project"},
					Model:  "gpt-5.4",
					CWD:    "/tmp/project",
				})
			default:
				writeError(t, enc, req.ID, "unexpected method "+req.Method)
			}
		}
	}()

	runtime := NewRuntime(RuntimeOptions{
		Enabled: true,
		Client:  NewClient(clientConn),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := runtime.StartSession(ctx, externalagents.StartSessionRequest{
		Model: "gpt-5.4",
		CWD:   "/tmp/project",
	}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}

	select {
	case params := <-gotParams:
		if params.ApprovalPolicy != defaultApprovalPolicy {
			t.Fatalf("approval policy = %q, want %q", params.ApprovalPolicy, defaultApprovalPolicy)
		}
		if params.Sandbox != defaultSandbox {
			t.Fatalf("sandbox = %q, want %q", params.Sandbox, defaultSandbox)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for thread/start params")
	}
}

func TestRuntimeResumesWhenTurnThreadIsNotLoaded(t *testing.T) {
	t.Run("missing rollout", func(t *testing.T) {
		testRuntimeResumesWhenTurnThreadIsNotLoaded(t, "no rollout found for thread id thread_1")
	})
	t.Run("thread not found", func(t *testing.T) {
		testRuntimeResumesWhenTurnThreadIsNotLoaded(t, "thread not found: thread_1")
	})
}

func testRuntimeResumesWhenTurnThreadIsNotLoaded(t *testing.T, startTurnError string) {
	t.Helper()

	clientConn, serverConn := pipePair()
	defer clientConn.Close()
	defer serverConn.Close()

	resumeCalled := make(chan ThreadResumeParams, 1)
	go func() {
		dec := json.NewDecoder(serverConn)
		enc := json.NewEncoder(serverConn)
		turnStartCalls := 0
		for {
			var req wireRequest
			if err := dec.Decode(&req); err != nil {
				return
			}
			switch req.Method {
			case "initialize":
				writeResult(t, enc, req.ID, InitializeResponse{
					UserAgent:      "codex-test/0",
					CodexHome:      "/tmp/codex",
					PlatformFamily: "unix",
					PlatformOS:     "linux",
				})
			case "initialized":
			case "turn/start":
				turnStartCalls++
				if turnStartCalls == 1 {
					writeError(t, enc, req.ID, startTurnError)
					continue
				}
				writeResult(t, enc, req.ID, TurnStartResponse{
					Turn: Turn{ID: "turn_1", ThreadID: "thread_1", Status: "running"},
				})
				writeNotification(t, enc, "item/agentMessage/delta", AgentMessageDelta{
					ThreadID: "thread_1",
					TurnID:   "turn_1",
					ItemID:   "item_1",
					Delta:    "pong",
				})
				writeNotification(t, enc, "turn/completed", TurnCompleted{
					ThreadID: "thread_1",
					Turn:     Turn{ID: "turn_1", ThreadID: "thread_1", Status: "completed"},
				})
			case "thread/resume":
				var params ThreadResumeParams
				if err := json.Unmarshal(req.Params, &params); err != nil {
					t.Errorf("decode thread/resume params: %v", err)
					return
				}
				resumeCalled <- params
				writeResult(t, enc, req.ID, ThreadResumeResponse{
					Thread: Thread{ID: "thread_1", SessionID: "session_1", CWD: "/tmp/project"},
					Model:  "gpt-5.4",
					CWD:    "/tmp/project",
				})
			default:
				writeError(t, enc, req.ID, "unexpected method "+req.Method)
			}
		}
	}()

	runtime := NewRuntime(RuntimeOptions{
		Enabled: true,
		Client:  NewClient(clientConn),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events, err := runtime.Send(ctx, externalagents.ExternalSession{
		AgentID:          AgentID,
		ExternalThreadID: "thread_1",
		CWD:              "/tmp/project",
		Model:            "gpt-5.4",
		ApprovalPolicy:   "never",
		Sandbox:          "danger-full-access",
	}, externalagents.Input{Text: "hello"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	var got []externalagents.Event
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 2 || got[0].Text != "pong" || got[1].Kind != externalagents.EventTurnCompleted {
		t.Fatalf("events = %+v, want message delta and completed", got)
	}

	select {
	case params := <-resumeCalled:
		if params.ThreadID != "thread_1" {
			t.Fatalf("resume thread id = %q, want thread_1", params.ThreadID)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for thread/resume")
	}
}
