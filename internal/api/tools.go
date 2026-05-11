package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, core.ToolsResponse{Tools: s.core.ListToolSpecs()})
}

func (s *Server) handleToolExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	var req core.ExecuteToolInput
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Approved {
		writeErrorMessage(w, http.StatusBadRequest, "tool approval must be resolved through /v1/approvals/{id}/resolve")
		return
	}

	req.Approved = false
	result, err := s.core.ExecuteTool(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, core.ToolExecuteResponse{Result: result})
}
