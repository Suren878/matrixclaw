package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionGoal(w http.ResponseWriter, r *http.Request) {
	response, err := s.Core.SessionGoal(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// handleSessionGoalSet sets the goal; a check command runs without asking, so
// only the owner sets one.
func (s *Server) handleSessionGoalSet(w http.ResponseWriter, r *http.Request) {
	var input core.GoalInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.CheckCommand != "" && roleOf(r) != core.RoleOwner {
		writeError(w, core.ErrOwnerOnly)
		return
	}
	response, err := s.Core.SetSessionGoal(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSessionGoalAction(w http.ResponseWriter, r *http.Request) {
	response, err := s.Core.ChangeSessionGoal(r.Context(), r.PathValue("id"), core.GoalAction(r.PathValue("action")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSessionGoalClear(w http.ResponseWriter, r *http.Request) {
	response, err := s.Core.ClearSessionGoal(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}
