package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleClientDeliveries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.core == nil {
		writeErrorMessage(w, http.StatusServiceUnavailable, "core is not configured")
		return
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeErrorMessage(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}
	var createdAfter time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("created_after")); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid created_after")
			return
		}
		createdAfter = parsed
	}
	deliveries, err := s.core.ListClientDeliveries(r.Context(), core.ClientDeliveryFilter{
		Client:       r.URL.Query().Get("client"),
		ExternalKey:  r.URL.Query().Get("external_key"),
		SessionID:    r.URL.Query().Get("session_id"),
		RunID:        r.URL.Query().Get("run_id"),
		TaskID:       r.URL.Query().Get("task_id"),
		Type:         r.URL.Query().Get("type"),
		Status:       core.ClientDeliveryStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		CreatedAfter: createdAfter,
		Limit:        limit,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.ClientDeliveriesResponse{Deliveries: deliveries})
}

func (s *Server) handleClientDeliveryByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.core == nil {
		writeErrorMessage(w, http.StatusServiceUnavailable, "core is not configured")
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/client-deliveries/"), "/")
	deliveryID, action, ok := strings.Cut(path, "/")
	deliveryID = strings.TrimSpace(deliveryID)
	action = strings.TrimSpace(action)
	if !ok || deliveryID == "" || strings.Contains(action, "/") {
		writeErrorMessage(w, http.StatusNotFound, "client delivery endpoint not found")
		return
	}
	switch action {
	case "ack":
		if err := s.core.AcknowledgeClientDelivery(r.Context(), deliveryID); err != nil {
			writeError(w, err)
			return
		}
	case "fail":
		var request core.ClientDeliveryFailRequest
		if !decodeOptionalJSONBody(w, r, &request) {
			return
		}
		var deliveryErr error
		if errText := strings.TrimSpace(request.Error); errText != "" {
			deliveryErr = errors.New(errText)
		}
		if err := s.core.MarkClientDeliveryFailed(r.Context(), core.ClientDelivery{ID: deliveryID}, deliveryErr); err != nil {
			writeError(w, err)
			return
		}
	default:
		writeErrorMessage(w, http.StatusNotFound, "client delivery endpoint not found")
		return
	}
	writeJSON(w, http.StatusOK, core.OKResponse{OK: true})
}
