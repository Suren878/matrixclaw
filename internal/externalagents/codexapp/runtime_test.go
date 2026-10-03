package codexapp

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func TestCompletedTurnOutcomePreservesFailure(t *testing.T) {
	details := "retry limit reached"
	kind, errText := completedTurnOutcome(Turn{
		Status: TurnStatusFailed,
		Error:  &TurnError{Message: "provider error", AdditionalDetails: &details},
	})
	if kind != externalagents.EventTurnFailed {
		t.Fatalf("kind = %q, want %q", kind, externalagents.EventTurnFailed)
	}
	if !strings.Contains(errText, "provider error") || !strings.Contains(errText, details) {
		t.Fatalf("error = %q, want primary message and details", errText)
	}
}

func TestCompletedTurnOutcomeDoesNotTreatInterruptedAsSuccess(t *testing.T) {
	kind, errText := completedTurnOutcome(Turn{Status: TurnStatusInterrupted})
	if kind != externalagents.EventTurnFailed {
		t.Fatalf("kind = %q, want %q", kind, externalagents.EventTurnFailed)
	}
	if !strings.Contains(errText, "interrupted") {
		t.Fatalf("error = %q, want interrupted", errText)
	}
}

func TestNormalizeRetryableErrorIsHeartbeat(t *testing.T) {
	events, done := normalizeNotification(Notification{
		Method: "error",
		Params: ErrorNotification{
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			Error:     TurnError{Message: "connection reset"},
			WillRetry: true,
		},
	}, "thread-1", "turn-1")
	if done {
		t.Fatal("retryable error must not terminate the turn")
	}
	if len(events) != 1 || events[0].Kind != externalagents.EventHeartbeat {
		t.Fatalf("events = %#v, want one heartbeat", events)
	}
}

func TestNormalizeNonRetryableErrorFailsTurn(t *testing.T) {
	events, done := normalizeNotification(Notification{
		Method: "error",
		Params: ErrorNotification{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Error:    TurnError{Message: "authentication failed"},
		},
	}, "thread-1", "turn-1")
	if !done {
		t.Fatal("non-retryable error must terminate the turn")
	}
	if len(events) != 1 || events[0].Kind != externalagents.EventTurnFailed {
		t.Fatalf("events = %#v, want one turn failure", events)
	}
}

func TestNormalizeTurnStartedIsNotTerminal(t *testing.T) {
	events, done := normalizeNotification(Notification{
		Method: "turn/started",
		Params: TurnStarted{
			ThreadID: "thread-1",
			Turn:     Turn{ID: "turn-1", Status: TurnStatusInProgress},
		},
	}, "thread-1", "turn-1")
	if done || len(events) != 1 || events[0].Kind != externalagents.EventHeartbeat {
		t.Fatalf("events = %#v done = %t, want non-terminal heartbeat", events, done)
	}
}

func TestRuntimeInterruptSendsActiveTurnID(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := NewClient(clientConn)
	runtime := NewRuntime(RuntimeOptions{Enabled: true, Client: client})
	t.Cleanup(func() {
		_ = runtime.Close()
		_ = client.Close()
		_ = serverConn.Close()
	})
	runtime.setActiveTurn("thread-1", "turn-1")

	requestReceived := make(chan map[string]json.RawMessage, 1)
	go func() {
		decoder := json.NewDecoder(serverConn)
		var request map[string]json.RawMessage
		if decoder.Decode(&request) != nil {
			return
		}
		requestReceived <- request
		id := request["id"]
		_, _ = serverConn.Write([]byte(`{"id":` + string(id) + `,"result":{}}` + "\n"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Interrupt(ctx, externalagents.ExternalSession{ExternalThreadID: "thread-1"}); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}

	select {
	case request := <-requestReceived:
		var method string
		if err := json.Unmarshal(request["method"], &method); err != nil {
			t.Fatalf("decode method: %v", err)
		}
		if method != "turn/interrupt" {
			t.Fatalf("method = %q, want turn/interrupt", method)
		}
		var params TurnInterruptParams
		if err := json.Unmarshal(request["params"], &params); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if params.ThreadID != "thread-1" || params.TurnID != "turn-1" {
			t.Fatalf("params = %#v, want active thread and turn", params)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for interrupt request")
	}
}

func TestNormalizeDeclinedCommandSaysWhy(t *testing.T) {
	events, _ := normalizeNotification(Notification{
		Method: "item/completed",
		Params: ItemNotification{ThreadID: "thread-1", TurnID: "turn-1", Item: json.RawMessage(`{"id":"i","type":"commandExecution","command":"touch x","status":"declined"}`)},
	}, "thread-1", "turn-1")
	if len(events) != 1 || !strings.Contains(events[0].Error, "permission mode") {
		t.Fatalf("events = %#v, want a declined tool explaining the permission mode", events)
	}
}
