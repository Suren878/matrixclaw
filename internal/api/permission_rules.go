package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionPermissionRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Core.SessionPermissionRules(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.PermissionRulesResponse{Rules: rules})
}

func (s *Server) handlePermissionRuleCreate(w http.ResponseWriter, r *http.Request) {
	var request core.PermissionRuleRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	rule, err := s.Core.AddPermissionRule(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.PermissionRuleResponse{Rule: rule})
}

func (s *Server) handlePermissionRuleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Core.DeletePermissionRule(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
