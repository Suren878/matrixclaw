package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

// handleSkills lists the library: matching query when set, by use with
// order=usage.
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, ok := queryInt(w, r, "limit", 0)
	if !ok {
		return
	}
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
		result, err = s.Skills.Usage(r.Context())
	case strings.TrimSpace(query.Get("query")) != "":
		result, err = s.Skills.Search(r.Context(), strings.TrimSpace(query.Get("query")), opts)
	default:
		result, err = s.Skills.List(r.Context(), opts)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, skills.SkillsResponse{Skills: result})
}

func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var req skills.InstallRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	installed, err := s.Skills.InstallPath(r.Context(), req.Path)
	if err != nil {
		writeError(w, withStatus(http.StatusBadRequest, err))
		return
	}
	writeJSON(w, http.StatusCreated, skills.SkillsResponse{Skills: installed})
}

func (s *Server) handleSkillDraft(w http.ResponseWriter, r *http.Request) {
	var req skills.DraftRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	draft, err := s.Skills.CreateDraft(r.Context(), req.Name, req.Description, req.Tags, req.Body)
	if err != nil {
		writeError(w, withStatus(http.StatusBadRequest, err))
		return
	}
	writeJSON(w, http.StatusCreated, draft)
}

func (s *Server) handleSkill(w http.ResponseWriter, r *http.Request) {
	detail, err := s.Skills.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleSkillUpdate(w http.ResponseWriter, r *http.Request) {
	var req skills.MetadataUpdate
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := s.Skills.UpdateMetadata(r.Context(), r.PathValue("id"), req)
	if err != nil {
		writeError(w, withStatus(http.StatusBadRequest, err))
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleSkillBody(w http.ResponseWriter, r *http.Request) {
	var req skills.BodyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeSkillResult(w, s.Skills.UpdateBody(r.Context(), r.PathValue("id"), req.Body))
}

func (s *Server) handleSkillAction(w http.ResponseWriter, r *http.Request) {
	action, ok := skills.ParseAction(r.PathValue("action"))
	if !ok {
		writeNotFound(w)
		return
	}
	writeSkillResult(w, s.Skills.Apply(r.Context(), r.PathValue("id"), action))
}

func (s *Server) handleSessionSkills(w http.ResponseWriter, r *http.Request) {
	items, err := s.Skills.SessionSkills(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, skills.SkillsResponse{Skills: items})
}

func (s *Server) handleSessionSkillUse(w http.ResponseWriter, r *http.Request) {
	detail, err := s.Skills.Use(r.Context(), r.PathValue("id"), r.PathValue("skill"))
	if err != nil {
		writeError(w, withStatus(http.StatusBadRequest, err))
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleSessionSkillUnload(w http.ResponseWriter, r *http.Request) {
	writeSkillResult(w, s.Skills.Unload(r.Context(), r.PathValue("id"), r.PathValue("skill")))
}

func writeSkillResult(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, withStatus(http.StatusBadRequest, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
