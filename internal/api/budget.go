package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionBudget(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		report, err := s.core.SessionBudget(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionBudgetResponse{Budget: report})
	case http.MethodPut:
		var budget core.SessionBudget
		if !decodeJSONBody(w, r, &budget) {
			return
		}
		report, err := s.core.UpdateSessionBudget(r.Context(), sessionID, budget)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionBudgetResponse{Budget: report})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}
