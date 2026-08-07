package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/coder/websocket"
)

func TestOutboundCallDoesNotDialBeforeRealtimeReady(t *testing.T) {
	var answerCalls atomic.Int32
	var originateCalls atomic.Int32
	var events *ariEventHub
	channelID := safeARIID("call_test-call")

	matrixclaw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/realtime-voice/sessions" {
			http.Error(w, "realtime unavailable", http.StatusServiceUnavailable)
			return
		}
		t.Fatalf("unexpected MatrixClaw request: %s %s", r.Method, r.URL.Path)
	}))
	defer matrixclaw.Close()

	ari := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/channels/"+channelID:
			originateCalls.Add(1)
			go func() {
				time.Sleep(10 * time.Millisecond)
				events.broadcast(ariEvent{Type: "StasisStart", Channel: &ariChannel{ID: channelID}})
			}()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/channels/"+channelID+"/answer":
			answerCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/channels/"+channelID:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected ARI request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ari.Close()

	server := NewServer(Config{
		ARIURL:          ari.URL,
		ARIPassword:     "secret",
		ARIApp:          defaultARIApp,
		SIPProfile:      defaultSIPProfile,
		CallerID:        "100",
		CallTimeout:     time.Second,
		MatrixclawURL:   matrixclaw.URL,
		MatrixclawToken: "token",
	})
	server.events.setReady(true)
	events = server.events

	now := time.Now().UTC()
	call := &Call{
		ID:        "call_test",
		Direction: "outbound",
		To:        "15551234567",
		Profile:   defaultSIPProfile,
		Status:    "queued",
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := server.runCallOnce(context.Background(), call, createCallRequest{To: call.To})
	if err == nil {
		t.Fatalf("runCallOnce returned nil error, want realtime setup failure")
	}
	if originateCalls.Load() != 0 {
		t.Fatalf("originate calls = %d, want 0 before realtime is ready", originateCalls.Load())
	}
	if answerCalls.Load() != 0 {
		t.Fatalf("answer calls = %d, want 0 before realtime is ready", answerCalls.Load())
	}
}

func TestOutboundCallDoesNotSendInitialPromptBeforeDialing(t *testing.T) {
	var promptBeforeOriginate atomic.Bool
	promptSent := make(chan struct{})
	channelID := safeARIID("call_test-call")

	matrixclaw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/realtime-voice/sessions":
			writeJSON(w, http.StatusCreated, realtime.SessionCreateResponse{Session: realtime.SessionInfo{
				ID:            "voice_test",
				Status:        realtime.SessionStatusCreated,
				ProviderID:    realtime.ProviderGemini,
				CoreSessionID: "session_test",
				InputAudio:    realtime.DefaultInputAudioFormat(),
				OutputAudio:   realtime.DefaultOutputAudioFormat(),
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/realtime-voice/sessions/voice_test/stream":
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				t.Errorf("accept realtime websocket: %v", err)
				return
			}
			defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
			ready := realtime.Event{V: realtime.ProtocolVersion, Type: realtime.EventSessionReady, VoiceSessionID: "voice_test"}
			body, err := json.Marshal(ready)
			if err != nil {
				t.Errorf("marshal ready event: %v", err)
				return
			}
			if err := conn.Write(r.Context(), websocket.MessageText, body); err != nil {
				t.Errorf("write ready event: %v", err)
				return
			}
			for {
				_, data, err := conn.Read(r.Context())
				if err != nil {
					return
				}
				var event realtime.Event
				if err := json.Unmarshal(data, &event); err != nil {
					t.Errorf("decode realtime event: %v", err)
					return
				}
				if event.Type == realtime.EventInputTextAppend {
					close(promptSent)
					return
				}
			}
		default:
			t.Fatalf("unexpected MatrixClaw request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer matrixclaw.Close()

	ari := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/channels/"+channelID:
			select {
			case <-promptSent:
				promptBeforeOriginate.Store(true)
			case <-time.After(300 * time.Millisecond):
			}
			http.Error(w, "stop before placing test call", http.StatusServiceUnavailable)
		default:
			t.Fatalf("unexpected ARI request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ari.Close()

	server := NewServer(Config{
		ARIURL:          ari.URL,
		ARIPassword:     "secret",
		ARIApp:          defaultARIApp,
		SIPProfile:      defaultSIPProfile,
		CallerID:        "100",
		CallTimeout:     time.Second,
		MatrixclawURL:   matrixclaw.URL,
		MatrixclawToken: "token",
	})
	server.events.setReady(true)

	now := time.Now().UTC()
	call := &Call{
		ID:        "call_test",
		Direction: "outbound",
		To:        "15551234567",
		Profile:   defaultSIPProfile,
		Status:    "queued",
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := server.runCallOnce(context.Background(), call, createCallRequest{
		To:             call.To,
		InitialMessage: "Здравствуйте.",
	})
	if err == nil {
		t.Fatalf("runCallOnce returned nil error, want originate failure")
	}
	if promptBeforeOriginate.Load() {
		t.Fatalf("initial prompt was sent before dialing")
	}
}
