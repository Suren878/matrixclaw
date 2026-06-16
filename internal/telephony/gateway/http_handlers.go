package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/calls", s.handleCalls)
	mux.HandleFunc("/v1/calls/", s.handleCallByID)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(s.cfg.GatewayToken)
		if token != "" && bearerToken(r.Header.Get("Authorization")) != token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	status := "ready"
	var problems []string
	if s.cfg.ARIPassword == "" {
		status = "not_ready"
		problems = append(problems, "ARI password is required")
	}
	if s.cfg.MatrixclawToken == "" {
		status = "not_ready"
		problems = append(problems, "MatrixClaw API token is required")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
	defer cancel()
	if s.cfg.ARIPassword != "" {
		if err := s.ari.probe(ctx); err != nil {
			status = "not_ready"
			problems = append(problems, "ARI: "+err.Error())
		}
	}
	if s.cfg.MatrixclawToken != "" {
		if err := s.probeMatrixclaw(ctx); err != nil {
			status = "not_ready"
			problems = append(problems, "MatrixClaw: "+err.Error())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                 status,
		"error":                  strings.Join(problems, "; "),
		"ari_app":                s.cfg.ARIApp,
		"profile":                s.cfg.SIPProfile,
		"rtp_bind":               s.cfg.RTPBind,
		"inbound_enabled":        s.cfg.InboundEnabled,
		"inbound_allowed":        len(s.cfg.InboundAllowed),
		"record_calls":           s.cfg.RecordCalls,
		"recording_format":       s.cfg.RecordingFormat,
		"recording_temp_storage": s.cfg.RecordingStorage,
	})
}

func (s *Server) handleCalls(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		calls := make([]CallSnapshot, 0, len(s.calls))
		for _, call := range s.calls {
			s.syncCallStats(call)
			calls = append(calls, callSnapshot(call))
		}
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{"calls": calls})
	case http.MethodPost:
		var req createCallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		call, err := s.startCall(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"call": call})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleCallByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/calls/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "call not found"})
		return
	}
	call, ok := s.call(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "call not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.syncCallStats(call)
		writeJSON(w, http.StatusOK, map[string]any{"call": callSnapshot(call)})
	case http.MethodDelete:
		call.cancel()
		writeJSON(w, http.StatusOK, map[string]any{"call": callSnapshot(call)})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}
