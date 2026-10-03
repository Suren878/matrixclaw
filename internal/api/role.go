package api

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

type roleKey struct{}

// withRole puts the caller's role (core.RoleHeader) in the request context.
// Every caller holds the API token, so the daemon trusts the header as it
// trusts the token; a request without it acts as the owner.
func withRole(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := core.Role(r.Header.Get(core.RoleHeader))
		switch role {
		case "":
			role = core.RoleOwner
		case core.RoleOwner, core.RoleMember, core.RoleGuest:
		default:
			writeErrorMessage(w, http.StatusBadRequest, "unknown "+core.RoleHeader+": "+string(role))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), roleKey{}, role)))
	})
}

func roleOf(r *http.Request) core.Role {
	if role, ok := r.Context().Value(roleKey{}).(core.Role); ok {
		return role
	}
	return core.RoleOwner
}

// ownerOnly refuses a route to anyone but the owner: settings, permission
// modes and the daemon itself.
func ownerOnly(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if roleOf(r) != core.RoleOwner {
			writeError(w, core.ErrOwnerOnly)
			return
		}
		handler(w, r)
	}
}

// mayUseSession answers 403 and returns false when a non-owner targets a
// session that runs tools without asking.
func (s *Server) mayUseSession(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	if roleOf(r) == core.RoleOwner {
		return true
	}
	session, err := s.Core.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, err)
		return false
	}
	if core.RunsUnattended(session) {
		writeError(w, core.ErrSessionRestricted)
		return false
	}
	return true
}

// keepsRules reports whether the caller may add or remove a rule of scope:
// session rules for everyone but guests, global ones for the owner.
func keepsRules(r *http.Request, scope permission.Scope) bool {
	switch roleOf(r) {
	case core.RoleOwner:
		return true
	case core.RoleMember:
		return scope != permission.ScopeGlobal
	default:
		return false
	}
}
