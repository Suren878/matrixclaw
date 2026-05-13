package codexapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestClientThreadAndTurnProtocol(t *testing.T) {
	clientConn, serverConn := pipePair()
	defer clientConn.Close()
	defer serverConn.Close()

	go serveCodexAppProtocol(t, serverConn)

	client := NewClient(clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	initResp, err := client.Initialize(ctx, InitializeParams{
		ClientInfo: ClientInfo{Name: "matrixclaw-test", Version: "0"},
		Capabilities: &InitializeCapabilities{
			ExperimentalAPI: true,
		},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initResp.CodexHome != "/tmp/codex" {
		t.Fatalf("unexpected codex home: %q", initResp.CodexHome)
	}

	threadResp, err := client.StartThread(ctx, ThreadStartParams{
		Model: "gpt-5.4",
		CWD:   "/tmp/project",
	})
	if err != nil {
		t.Fatalf("start thread: %v", err)
	}
	if threadResp.Thread.ID != "thread_1" {
		t.Fatalf("unexpected thread id: %q", threadResp.Thread.ID)
	}

	turnResp, err := client.StartTurn(ctx, TurnStartParams{
		ThreadID: threadResp.Thread.ID,
		Input:    []UserInput{TextInput("hello")},
	})
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	if turnResp.Turn.ID != "turn_1" {
		t.Fatalf("unexpected turn id: %q", turnResp.Turn.ID)
	}

	select {
	case event := <-client.Events():
		if event.Method != "item/agentMessage/delta" {
			t.Fatalf("unexpected event method: %s", event.Method)
		}
		delta, ok := event.Params.(AgentMessageDelta)
		if !ok {
			t.Fatalf("unexpected event params type: %T", event.Params)
		}
		if delta.Delta != "pong" {
			t.Fatalf("unexpected delta: %q", delta.Delta)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestClientReturnsRPCError(t *testing.T) {
	clientConn, serverConn := pipePair()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		dec := json.NewDecoder(serverConn)
		enc := json.NewEncoder(serverConn)
		var req wireRequest
		if err := dec.Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if err := enc.Encode(map[string]any{
			"id": req.ID,
			"error": map[string]any{
				"code":    -32000,
				"message": "boom",
			},
		}); err != nil {
			t.Errorf("encode error: %v", err)
		}
	}()

	client := NewClient(clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Call(ctx, "thread/start", ThreadStartParams{}, nil)
	if err == nil {
		t.Fatal("expected rpc error")
	}
}

func TestLiveInitialize(t *testing.T) {
	if os.Getenv("MATRIXCLAW_CODEXAPP_LIVE") != "1" {
		t.Skip("set MATRIXCLAW_CODEXAPP_LIVE=1 to run against local codex app-server")
	}
	if !Available("") {
		t.Skip("codex binary not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := Start(ctx, ProcessOptions{Stderr: io.Discard})
	if err != nil {
		t.Fatalf("start codex app-server: %v", err)
	}
	defer client.Close()

	resp, err := client.Initialize(ctx, InitializeParams{
		ClientInfo: ClientInfo{Name: "matrixclaw-live-test", Version: "0"},
		Capabilities: &InitializeCapabilities{
			ExperimentalAPI: true,
		},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if resp.CodexHome == "" {
		t.Fatal("empty codex home")
	}
}

func TestLiveThreadTurn(t *testing.T) {
	if os.Getenv("MATRIXCLAW_CODEXAPP_LIVE_TURN") != "1" {
		t.Skip("set MATRIXCLAW_CODEXAPP_LIVE_TURN=1 to run a real Codex thread and turn")
	}
	if !Available("") {
		t.Skip("codex binary not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client, err := Start(ctx, ProcessOptions{Stderr: io.Discard})
	if err != nil {
		t.Fatalf("start codex app-server: %v", err)
	}
	defer client.Close()

	_, err = client.Initialize(ctx, InitializeParams{
		ClientInfo: ClientInfo{Name: "matrixclaw-live-turn-test", Version: "0"},
		Capabilities: &InitializeCapabilities{
			ExperimentalAPI: true,
		},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	thread, err := client.StartThread(ctx, ThreadStartParams{
		CWD:            t.TempDir(),
		ApprovalPolicy: "never",
		Sandbox:        "danger-full-access",
	})
	if err != nil {
		t.Fatalf("start thread: %v", err)
	}
	if thread.Thread.ID == "" {
		t.Fatal("empty thread id")
	}

	turn, err := client.StartTurn(ctx, TurnStartParams{
		ThreadID: thread.Thread.ID,
		Input: []UserInput{
			TextInput("Reply with exactly this text and nothing else: matrixclaw-codex-ok"),
		},
	})
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	if turn.Turn.ID == "" {
		t.Fatal("empty turn id")
	}

	var text string
	for {
		select {
		case event, ok := <-client.Events():
			if !ok {
				t.Fatalf("event stream closed before turn completed; text=%q err=%v", text, client.Err())
			}
			switch params := event.Params.(type) {
			case AgentMessageDelta:
				text += params.Delta
			case TurnCompleted:
				if params.ThreadID != thread.Thread.ID {
					t.Fatalf("unexpected completed thread id: %q", params.ThreadID)
				}
				if text == "" {
					t.Fatal("turn completed without assistant text")
				}
				return
			}
		case <-ctx.Done():
			t.Fatalf("waiting for live turn: %v; text=%q", ctx.Err(), text)
		}
	}
}

type wireRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func serveCodexAppProtocol(t *testing.T, conn io.ReadWriter) {
	t.Helper()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
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
			writeResult(t, enc, req.ID, ThreadStartResponse{
				Thread: Thread{
					ID:            "thread_1",
					SessionID:     "session_1",
					ModelProvider: "openai",
					CWD:           "/tmp/project",
					Status:        "idle",
				},
				Model:         "gpt-5.4",
				ModelProvider: "openai",
				CWD:           "/tmp/project",
			})
		case "turn/start":
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
		default:
			writeError(t, enc, req.ID, "unexpected method "+req.Method)
		}
	}
}

func writeResult(t *testing.T, enc *json.Encoder, id string, result any) {
	t.Helper()
	if err := enc.Encode(map[string]any{"id": id, "result": result}); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return
		}
		t.Errorf("encode result: %v", err)
	}
}

func writeError(t *testing.T, enc *json.Encoder, id string, message string) {
	t.Helper()
	if err := enc.Encode(map[string]any{
		"id": id,
		"error": map[string]any{
			"code":    -32000,
			"message": message,
		},
	}); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return
		}
		t.Errorf("encode error: %v", err)
	}
}

func writeNotification(t *testing.T, enc *json.Encoder, method string, params any) {
	t.Helper()
	if err := enc.Encode(map[string]any{"method": method, "params": params}); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return
		}
		t.Errorf("encode notification: %v", err)
	}
}

func pipePair() (io.ReadWriteCloser, io.ReadWriteCloser) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	return pipeConn{reader: clientReader, writer: clientWriter}, pipeConn{reader: serverReader, writer: serverWriter}
}

type pipeConn struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (c pipeConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c pipeConn) Write(p []byte) (int, error) {
	return c.writer.Write(p)
}

func (c pipeConn) Close() error {
	_ = c.reader.Close()
	return c.writer.Close()
}
