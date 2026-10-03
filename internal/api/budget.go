package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionBudget(w http.ResponseWriter, r *http.Request) {
	report, err := s.Core.SessionBudget(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionBudgetResponse{Budget: report})
}

func (s *Server) handleSessionBudgetUpdate(w http.ResponseWriter, r *http.Request) {
	var budget core.SessionBudget
	if !decodeJSON(w, r, &budget) {
		return
	}
	report, err := s.Core.UpdateSessionBudget(r.Context(), r.PathValue("id"), budget)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionBudgetResponse{Budget: report})
}
