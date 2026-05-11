package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleRunByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
	switch {
	case r.Method == http.MethodGet:
		runID := path
		run, err := s.core.GetRun(r.Context(), runID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.RunResponse{Run: run})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/cancel"):
		runID := strings.TrimSuffix(path, "/cancel")
		runID = strings.TrimSuffix(runID, "/")
		if runID == "" {
			writeErrorMessage(w, http.StatusBadRequest, "run id is required")
			return
		}
		run, err := s.core.CancelRun(r.Context(), runID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.RunResponse{Run: run})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}
