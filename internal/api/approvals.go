package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleApprovals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	state := core.ApprovalState(strings.TrimSpace(r.URL.Query().Get("state")))
	approvals, err := s.core.ListApprovals(r.Context(), r.URL.Query().Get("session_id"), state)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.ApprovalsResponse{Approvals: approvals})
}

func (s *Server) handleApprovalByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	approvalID := strings.TrimPrefix(r.URL.Path, "/v1/approvals/")
	approvalID = strings.TrimSuffix(approvalID, "/resolve")
	approvalID = strings.Trim(approvalID, "/")
	if approvalID == "" || !strings.HasSuffix(r.URL.Path, "/resolve") {
		writeErrorMessage(w, http.StatusNotFound, "approval endpoint not found")
		return
	}

	var req core.ApprovalResolveRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	approval, err := s.core.ResolveApproval(r.Context(), approvalID, req.Approved)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, core.ApprovalResponse{Approval: approval})
}
