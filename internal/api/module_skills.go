package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

// handleSkills lists the library: matching query when set, by use with
// order=usage.
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, _ := strconv.Atoi(strings.TrimSpace(query.Get("limit")))
	opts := skills.SearchOptions{
		Limit:              limit,
		IncludeQuarantined: query.Get("include_quarantined") == "1",
		IncludeArchived:    query.Get("include_archived") == "1",
		IncludeDisabled:    query.Get("include_disabled") == "1",
	}
	var result []skills.Skill
	var err error
	switch {
	case query.Get("order") == "usage":
		result, err = s.Skills.Usage()
	case strings.TrimSpace(query.Get("query")) != "":
		result, err = s.Skills.Search(strings.TrimSpace(query.Get("query")), opts)
	default:
		result, err = s.Skills.List(opts)
	}
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, skills.SkillsResponse{Skills: result})
}

func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var req skills.InstallRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	installed, err := s.Skills.InstallPath(req.Path, skills.InstallOptions{Provenance: req.Path})
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, skills.SkillsResponse{Skills: installed})
}

func (s *Server) handleSkillDraft(w http.ResponseWriter, r *http.Request) {
	var req skills.DraftRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	draft, err := s.Skills.CreateDraft(req.Name, req.Description, req.Tags, req.Body)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, draft)
}

func (s *Server) handleSkill(w http.ResponseWriter, r *http.Request) {
	detail, err := s.Skills.Get(r.PathValue("id"))
	if err != nil {
		writeErrorMessage(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
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
	var req skills.BodyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeSkillResult(w, s.Skills.UpdateBody(r.PathValue("id"), req.Body))
}

func (s *Server) handleSkillDelete(w http.ResponseWriter, r *http.Request) {
	writeSkillResult(w, s.Skills.Remove(r.PathValue("id")))
}

func (s *Server) handleSkillAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "trust":
		err = s.Skills.Trust(id)
	case "quarantine":
		err = s.Skills.Quarantine(id)
	case "disable":
		err = s.Skills.Disable(id)
	case "enable":
		err = s.Skills.SetEnabled(id, true)
	case "archive":
		err = s.Skills.Archive(id)
	case "restore":
		err = s.Skills.Restore(id)
	case "pin":
		err = s.Skills.Pin(id, true)
	case "unpin":
		err = s.Skills.Pin(id, false)
	default:
		writeNotFound(w)
		return
	}
	writeSkillResult(w, err)
}

func (s *Server) handleSessionSkills(w http.ResponseWriter, r *http.Request) {
	items, err := s.Skills.SessionSkills(r.PathValue("id"))
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, skills.SkillsResponse{Skills: items})
}

func (s *Server) handleSessionSkillUse(w http.ResponseWriter, r *http.Request) {
	detail, err := s.Skills.Use(r.PathValue("id"), r.PathValue("skill"))
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleSessionSkillUnload(w http.ResponseWriter, r *http.Request) {
	writeSkillResult(w, s.Skills.Unload(r.PathValue("id"), r.PathValue("skill")))
}

func writeSkillResult(w http.ResponseWriter, err error) {
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
