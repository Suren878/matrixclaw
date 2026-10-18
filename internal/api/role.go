package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

// policy says who may use a route: anyone (open), only the owner (owner), or
// anyone except in a session that runs tools without asking
// (core.RunsUnattended), which only the owner may act in (session).
type policy struct {
	name      string
	ownerOnly bool
	// session finds the session a request acts in; "" when it names none.
	session func(*Server, *http.Request) (string, error)
}

var (
	open  = policy{name: "open"}
	owner = policy{name: "owner", ownerOnly: true}
	// The session is the path's {id}, or that of the run, task, approval,
	// automation job or permission rule it names, or the one the JSON body names (session_id, or
	// the client's binding).
	sessionInPath = policy{name: "session(id)", session: func(_ *Server, r *http.Request) (string, error) {
		return r.PathValue("id"), nil
	}}
	sessionOfRun = policy{name: "session(run)", session: func(s *Server, r *http.Request) (string, error) {
		run, err := s.Core.GetRun(r.Context(), r.PathValue("id"))
		return run.SessionID, err
	}}
	sessionOfTask = policy{name: "session(task)", session: func(s *Server, r *http.Request) (string, error) {
		detail, err := s.Core.TaskDetail(r.Context(), r.PathValue("id"))
		return detail.Task.SessionID, err
	}}
	sessionOfApproval = policy{name: "session(approval)", session: func(s *Server, r *http.Request) (string, error) {
		approval, err := s.Core.GetApproval(r.Context(), r.PathValue("id"))
		return approval.SessionID, err
	}}
	sessionOfJob = policy{name: "session(job)", session: func(s *Server, r *http.Request) (string, error) {
		job, err := s.Automation.GetJob(r.Context(), r.PathValue("id"))
		return job.SessionID, err
	}}
	// A global rule names no session; its handler keeps it for the owner.
	sessionOfRule = policy{name: "session(rule)", session: func(s *Server, r *http.Request) (string, error) {
		rule, err := s.Core.GetPermissionRule(r.Context(), r.PathValue("id"))
		return rule.SessionID, err
	}}
	sessionInBody = policy{name: "session(body)", session: bodySession}
)

// authorize enforces the route's policy; the owner passes every one.
func (s *Server) authorize(p policy, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if roleOf(r) == core.RoleOwner {
			next(w, r)
			return
		}
		if p.ownerOnly {
			writeError(w, core.ErrOwnerOnly)
			return
		}
		if p.session != nil {
			if err := s.refuseUnattended(r, p.session); err != nil {
				writeError(w, err)
				return
			}
		}
		next(w, r)
	}
}

// refuseUnattended answers core.ErrSessionRestricted for a request that acts
// in a session that RunsUnattended. A session that cannot be found is left to
// the handler, which answers as it would for anyone.
func (s *Server) refuseUnattended(r *http.Request, find func(*Server, *http.Request) (string, error)) error {
	id, err := find(s, r)
	if err == nil && id != "" {
		var session core.Session
		if session, err = s.Core.GetSession(r.Context(), id); err == nil && core.RunsUnattended(session) {
			return core.ErrSessionRestricted
		}
	}
	if errors.Is(err, core.ErrNotFound) || errors.Is(err, core.ErrBindingNotFound) || errors.Is(err, core.ErrSessionSelectionRequired) {
		return nil
	}
	return err
}

// bodySession is the session a JSON body names: its session_id, or else the
// binding of its client (and the only session when it allows auto-binding).
// The body is put back for the handler; one it cannot decode names none.
func bodySession(s *Server, r *http.Request) (string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, defaultJSONBodyLimitBytes+1))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
	if err != nil {
		return "", err
	}
	if int64(len(body)) > defaultJSONBodyLimitBytes {
		return "", errBodyTooLarge
	}
	var target struct {
		SessionID        string `json:"session_id"`
		Client           string `json:"client"`
		ExternalKey      string `json:"external_key"`
		AllowAutoBindOne bool   `json:"allow_auto_bind_one"`
	}
	if len(body) == 0 || json.Unmarshal(body, &target) != nil {
		return "", nil
	}
	session, err := s.Core.MessageSession(r.Context(), core.HandleMessageInput{
		SessionID: target.SessionID, Client: target.Client, ExternalKey: target.ExternalKey, AllowAutoBindOne: target.AllowAutoBindOne,
	})
	return session.ID, err
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
