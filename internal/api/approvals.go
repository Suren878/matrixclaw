package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleApprovals(w http.ResponseWriter, r *http.Request) {
	state := core.ApprovalState(strings.TrimSpace(r.URL.Query().Get("state")))
	approvals, err := s.Core.ListApprovals(r.Context(), r.URL.Query().Get("session_id"), state)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.ApprovalsResponse{Approvals: approvals})
}

func (s *Server) handleApprovalResolve(w http.ResponseWriter, r *http.Request) {
	var req core.ApprovalResolveRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	approval, err := s.Core.ResolveApproval(r.Context(), r.PathValue("id"), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.ApprovalResponse{Approval: approval})
}
