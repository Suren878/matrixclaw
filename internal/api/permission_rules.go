package api

import (
	"net/http"
	"slices"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
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
	ruleID := r.PathValue("id")
	scope := permission.ScopeSession
	if roleOf(r) != core.RoleOwner {
		global, err := s.Core.GlobalPermissionRules(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		if slices.ContainsFunc(global, func(rule permission.Rule) bool { return rule.ID == ruleID }) {
			scope = permission.ScopeGlobal
		}
	}
	if !keepsRules(r, scope) {
		writeError(w, core.ErrOwnerOnly)
		return
	}
	if err := s.Core.DeletePermissionRule(r.Context(), ruleID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
