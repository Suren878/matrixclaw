package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if s.skillsUnavailable(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		opts := skills.SearchOptions{
			Limit:              limit,
			IncludeQuarantined: truthyQuery(r.URL.Query().Get("include_quarantined")),
			IncludeArchived:    truthyQuery(r.URL.Query().Get("include_archived")),
			IncludeDisabled:    truthyQuery(r.URL.Query().Get("include_disabled")),
		}
		var result []skills.Skill
		var err error
		if query != "" {
			result, err = s.skills.Search(query, opts)
		} else {
			result, err = s.skills.List(opts)
		}
		if err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"skills": result})
	case http.MethodPost:
		var req struct {
			Path        string   `json:"path"`
			TrustState  string   `json:"trust_state,omitempty"`
			Name        string   `json:"name,omitempty"`
			Description string   `json:"description,omitempty"`
			Tags        []string `json:"tags,omitempty"`
			Body        string   `json:"body,omitempty"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Path) == "" {
			draft, err := s.skills.CreateDraft(req.Name, req.Description, req.Tags, req.Body)
			if err != nil {
				writeErrorMessage(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, draft)
			return
		}
		installed, err := s.skills.InstallPath(req.Path, skills.InstallOptions{Provenance: req.Path, TrustState: req.TrustState})
		if err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"skills": installed})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func truthyQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Server) handleSkillByID(w http.ResponseWriter, r *http.Request) {
	if s.skillsUnavailable(w) {
		return
	}
	raw := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/modules/skills/"))
	if raw == "" {
		writeNotFound(w)
		return
	}
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	if raw == "usage" && r.Method == http.MethodGet {
		usage, err := s.skills.Usage()
		if err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, usage)
		return
	}
	if raw == "curator" && r.Method == http.MethodPost {
		result, err := s.skills.Curator()
		if err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if strings.HasPrefix(raw, "sessions/") {
		s.handleSessionSkills(w, r, strings.TrimPrefix(raw, "sessions/"))
		return
	}
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch {
	case r.Method == http.MethodGet && action == "":
		detail, err := s.skills.Get(id)
		if err != nil {
			writeErrorMessage(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, detail)
	case r.Method == http.MethodDelete && action == "":
		if err := s.skills.Remove(id); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case r.Method == http.MethodPatch && action == "":
		var req skills.MetadataUpdate
		if !decodeJSONBody(w, r, &req) {
			return
		}
		updated, err := s.skills.UpdateMetadata(id, req)
		if err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case r.Method == http.MethodPatch && action == "body":
		var req struct {
			Body string `json:"body"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if err := s.skills.UpdateBody(id, req.Body); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case r.Method == http.MethodPost && action != "":
		if err := s.applySkillAction(id, action); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete)
	}
}

func (s *Server) handleSessionSkills(w http.ResponseWriter, r *http.Request, raw string) {
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeNotFound(w)
		return
	}
	sessionID, err := url.PathUnescape(parts[0])
	if err != nil {
		sessionID = parts[0]
	}
	switch {
	case r.Method == http.MethodGet && len(parts) == 1:
		items, err := s.skills.SessionSkills(sessionID)
		if err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"skills": items})
	case r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "use":
		skillID, err := url.PathUnescape(parts[1])
		if err != nil {
			skillID = parts[1]
		}
		detail, err := s.skills.Use(sessionID, skillID)
		if err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, detail)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "unload":
		skillID, err := url.PathUnescape(parts[1])
		if err != nil {
			skillID = parts[1]
		}
		if err := s.skills.Unload(sessionID, skillID); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) applySkillAction(id string, action string) error {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "trust":
		return s.skills.Trust(id)
	case "quarantine":
		return s.skills.Quarantine(id)
	case "disable":
		return s.skills.Disable(id)
	case "enable":
		return s.skills.SetEnabled(id, true)
	case "archive":
		return s.skills.Archive(id)
	case "restore":
		return s.skills.Restore(id)
	case "pin":
		return s.skills.Pin(id, true)
	case "unpin":
		return s.skills.Pin(id, false)
	default:
		return errUnknownSkillAction(action)
	}
}

type errUnknownSkillAction string

func (e errUnknownSkillAction) Error() string {
	return "unknown skill action: " + string(e)
}

func (s *Server) skillsUnavailable(w http.ResponseWriter) bool {
	if s.skills != nil {
		return false
	}
	writeErrorMessage(w, http.StatusServiceUnavailable, "skills module is not configured")
	return true
}
