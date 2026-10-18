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
	if !keepsRules(r, request.Scope) {
		writeError(w, core.ErrOwnerOnly)
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
	rule, err := s.Core.GetPermissionRule(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if !keepsRules(r, rule.Scope) {
		writeError(w, core.ErrOwnerOnly)
		return
	}
	if err := s.Core.DeletePermissionRule(r.Context(), rule.ID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
