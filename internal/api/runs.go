package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.Core.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.RunResponse{Run: run})
}

func (s *Server) handleRunSteps(w http.ResponseWriter, r *http.Request) {
	steps, err := s.Core.RunSteps(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.RunStepsResponse{Steps: steps})
}

func (s *Server) handleRunProgress(w http.ResponseWriter, r *http.Request) {
	progress, err := s.Core.RunProgress(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.RunProgressResponse{Progress: progress})
}

func (s *Server) handleRunCancel(w http.ResponseWriter, r *http.Request) {
	run, err := s.Core.CancelRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.RunResponse{Run: run})
}
