package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleTools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, core.ToolsResponse{Tools: s.Core.ListToolSpecs()})
}

func (s *Server) handleToolExecute(w http.ResponseWriter, r *http.Request) {
	var req core.ExecuteToolInput
	if !decodeJSON(w, r, &req) {
		return
	}
	if !s.mayUseSession(w, r, req.SessionID) {
		return
	}

	result, err := s.Core.ExecuteTool(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, core.ToolExecuteResponse{Result: result})
}
