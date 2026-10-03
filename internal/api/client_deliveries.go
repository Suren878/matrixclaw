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
	deliveries, err := s.Core.ListClientDeliveries(r.Context(), core.ClientDeliveryFilter{
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

func (s *Server) handleClientDeliveryAck(w http.ResponseWriter, r *http.Request) {
	if err := s.Core.AcknowledgeClientDelivery(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.OKResponse{OK: true})
}

func (s *Server) handleClientDeliveryFail(w http.ResponseWriter, r *http.Request) {
	var request core.ClientDeliveryFailRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	var deliveryErr error
	if errText := strings.TrimSpace(request.Error); errText != "" {
		deliveryErr = errors.New(errText)
	}
	if err := s.Core.MarkClientDeliveryFailed(r.Context(), core.ClientDelivery{ID: r.PathValue("id")}, deliveryErr); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.OKResponse{OK: true})
}
