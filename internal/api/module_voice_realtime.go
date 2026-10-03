package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/coder/websocket"
)

const realtimeVoiceWebSocketReadLimit = 8 << 20

func (s *Server) handleRealtimeVoiceModule(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, realtime.ModuleResponse{Module: s.Realtime.Descriptor(r.Context())})
}

func (s *Server) handleRealtimeVoiceSessionCreate(w http.ResponseWriter, r *http.Request) {
	var req realtime.SessionCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	session, err := s.Realtime.CreateSession(r.Context(), req)
	if err != nil {
		writeRealtimeVoiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, realtime.SessionCreateResponse{Session: session})
}

func (s *Server) handleRealtimeVoiceSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.Realtime.Session(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRealtimeVoiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, realtime.SessionResponse{Session: session})
}

func (s *Server) handleRealtimeVoiceSessionClose(w http.ResponseWriter, r *http.Request) {
	session, err := s.Realtime.CloseSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRealtimeVoiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, realtime.SessionResponse{Session: session})
}

func (s *Server) handleRealtimeVoiceStream(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(realtimeVoiceWebSocketReadLimit)
	stream := &realtimeWebSocketStream{conn: conn}
	if err := s.Realtime.ServeStream(r.Context(), r.PathValue("id"), stream); err != nil {
		_ = stream.Close(err)
	}
}

type realtimeWebSocketStream struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (s *realtimeWebSocketStream) Read(ctx context.Context) (realtime.Event, error) {
	messageType, data, err := s.conn.Read(ctx)
	if err != nil {
		return realtime.Event{}, err
	}
	if messageType != websocket.MessageText {
		return realtime.Event{}, fmt.Errorf("realtime voice websocket expected text message")
	}
	var event realtime.Event
	if err := json.Unmarshal(data, &event); err != nil {
		return realtime.Event{}, err
	}
	return event, nil
}

func (s *realtimeWebSocketStream) Write(ctx context.Context, event realtime.Event) error {
	if event.V == 0 {
		event.V = realtime.ProtocolVersion
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.Write(ctx, websocket.MessageText, body)
}

func (s *realtimeWebSocketStream) Close(reason error) error {
	status := websocket.StatusNormalClosure
	message := ""
	if reason != nil {
		status = websocket.StatusInternalError
		message = strings.TrimSpace(reason.Error())
		if len(message) > 120 {
			message = message[:120]
		}
	}
	return s.conn.Close(status, message)
}

func writeRealtimeVoiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, realtime.ErrDisabled):
		writeErrorMessage(w, http.StatusConflict, err.Error())
	case errors.Is(err, realtime.ErrInvalidRequest):
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, realtime.ErrSessionNotFound):
		writeErrorMessage(w, http.StatusNotFound, err.Error())
	case errors.Is(err, realtime.ErrProviderUnavailable):
		writeErrorMessage(w, http.StatusConflict, err.Error())
	default:
		writeErrorMessage(w, http.StatusBadGateway, err.Error())
	}
}
