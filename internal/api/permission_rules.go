package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionPermissionRules(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		rules, err := s.core.SessionPermissionRules(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.PermissionRulesResponse{Rules: rules})
	case http.MethodPost:
		var request core.PermissionRuleRequest
		if !decodeJSONBody(w, r, &request) {
			return
		}
		rule, err := s.core.AddPermissionRule(r.Context(), sessionID, request)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.PermissionRuleResponse{Rule: rule})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handlePermissionRuleByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeMethodNotAllowed(w, http.MethodDelete)
		return
	}
	ruleID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/permission-rules/"), "/")
	if ruleID == "" || strings.Contains(ruleID, "/") {
		writeNotFound(w)
		return
	}
	if err := s.core.DeletePermissionRule(r.Context(), ruleID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
