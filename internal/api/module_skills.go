package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
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
		result, err = s.Skills.Search(query, opts)
	} else {
		result, err = s.Skills.List(opts)
	}
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": result})
}

func (s *Server) handleSkillCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path        string   `json:"path"`
		Name        string   `json:"name,omitempty"`
		Description string   `json:"description,omitempty"`
		Tags        []string `json:"tags,omitempty"`
		Body        string   `json:"body,omitempty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		draft, err := s.Skills.CreateDraft(req.Name, req.Description, req.Tags, req.Body)
		if err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, draft)
		return
	}
	installed, err := s.Skills.InstallPath(req.Path, skills.InstallOptions{Provenance: req.Path})
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"skills": installed})
}

func truthyQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Server) handleSkillUsage(w http.ResponseWriter, _ *http.Request) {
	usage, err := s.Skills.Usage()
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (s *Server) handleSkill(w http.ResponseWriter, r *http.Request) {
	detail, err := s.Skills.Get(r.PathValue("id"))
	if err != nil {
		writeErrorMessage(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleSkillDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Skills.Remove(r.PathValue("id")); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSkillUpdate(w http.ResponseWriter, r *http.Request) {
	var req skills.MetadataUpdate
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := s.Skills.UpdateMetadata(r.PathValue("id"), req)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleSkillBody(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.Skills.UpdateBody(r.PathValue("id"), req.Body); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSkillAction(w http.ResponseWriter, r *http.Request) {
	if err := s.applySkillAction(r.PathValue("id"), r.PathValue("action")); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSessionSkills(w http.ResponseWriter, r *http.Request) {
	items, err := s.Skills.SessionSkills(r.PathValue("session"))
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": items})
}

func (s *Server) handleSessionSkillUse(w http.ResponseWriter, r *http.Request) {
	detail, err := s.Skills.Use(r.PathValue("session"), r.PathValue("skill"))
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleSessionSkillUnload(w http.ResponseWriter, r *http.Request) {
	if err := s.Skills.Unload(r.PathValue("session"), r.PathValue("skill")); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) applySkillAction(id string, action string) error {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "trust":
		return s.Skills.Trust(id)
	case "quarantine":
		return s.Skills.Quarantine(id)
	case "disable":
		return s.Skills.Disable(id)
	case "enable":
		return s.Skills.SetEnabled(id, true)
	case "archive":
		return s.Skills.Archive(id)
	case "restore":
		return s.Skills.Restore(id)
	case "pin":
		return s.Skills.Pin(id, true)
	case "unpin":
		return s.Skills.Pin(id, false)
	default:
		return errUnknownSkillAction(action)
	}
}

type errUnknownSkillAction string

func (e errUnknownSkillAction) Error() string {
	return "unknown skill action: " + string(e)
}
