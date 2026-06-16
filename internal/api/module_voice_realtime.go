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
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/coder/websocket"
)

const realtimeVoiceWebSocketReadLimit = 8 << 20

func (s *Server) handleRealtimeVoiceModule(w http.ResponseWriter, r *http.Request) {
	if s.realtimeVoice == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "realtime voice service is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, realtime.ModuleResponse{Module: s.realtimeVoice.Descriptor(r.Context())})
	case http.MethodPatch:
		if s.setup == nil {
			writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
			return
		}
		var update setup.VoiceModuleUpdate
		if !decodeJSONBody(w, r, &update) {
			return
		}
		if _, err := s.setup.UpdateVoiceModule(setup.VoiceModuleRealtime, update); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		if s.adminReload != nil {
			if err := s.adminReload(r.Context()); err != nil {
				writeErrorMessage(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, realtime.ModuleResponse{Module: s.realtimeVoice.Descriptor(r.Context())})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (s *Server) handleRealtimeVoiceSessions(w http.ResponseWriter, r *http.Request) {
	if s.realtimeVoice == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "realtime voice service is not configured")
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var req realtime.SessionCreateRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	session, err := s.realtimeVoice.CreateSession(r.Context(), req)
	if err != nil {
		writeRealtimeVoiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, realtime.SessionCreateResponse{Session: session})
}

func (s *Server) handleRealtimeVoiceSessionByID(w http.ResponseWriter, r *http.Request) {
	if s.realtimeVoice == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "realtime voice service is not configured")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/realtime-voice/sessions/"), "/")
	sessionID, suffix, _ := strings.Cut(rest, "/")
	if sessionID == "" {
		writeNotFound(w)
		return
	}
	if suffix == "stream" {
		s.handleRealtimeVoiceStream(w, r, sessionID)
		return
	}
	if suffix != "" {
		writeNotFound(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		session, err := s.realtimeVoice.Session(r.Context(), sessionID)
		if err != nil {
			writeRealtimeVoiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, realtime.SessionResponse{Session: session})
	case http.MethodDelete:
		session, err := s.realtimeVoice.CloseSession(r.Context(), sessionID)
		if err != nil {
			writeRealtimeVoiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, realtime.SessionResponse{Session: session})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *Server) handleRealtimeVoiceStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(realtimeVoiceWebSocketReadLimit)
	stream := &realtimeWebSocketStream{conn: conn}
	if err := s.realtimeVoice.ServeStream(r.Context(), sessionID, stream); err != nil {
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
