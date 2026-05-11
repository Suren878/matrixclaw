package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/automation"
)

func (s *Server) handleAutomationJobs(w http.ResponseWriter, r *http.Request) {
	if s.automation == nil {
		writeErrorMessage(w, http.StatusServiceUnavailable, "automation is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}
		jobs, err := s.automation.ListJobs(r.Context(), automation.JobFilter{
			Status:    automation.JobStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
			SessionID: strings.TrimSpace(r.URL.Query().Get("session_id")),
			Limit:     limit,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, automation.JobsResponse{Jobs: jobs})
	case http.MethodPost:
		var request automation.CreateJobRequest
		if !decodeJSONBody(w, r, &request) {
			return
		}
		input, err := request.CreateInput()
		if err != nil {
			writeError(w, err)
			return
		}
		job, err := s.automation.CreateJob(r.Context(), input)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, automation.JobResponse{Job: job})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleAutomationJobByID(w http.ResponseWriter, r *http.Request) {
	if s.automation == nil {
		writeErrorMessage(w, http.StatusServiceUnavailable, "automation is not configured")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/automation/jobs/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeNotFound(w)
		return
	}
	jobID := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			job, err := s.automation.GetJob(r.Context(), jobID)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, automation.JobResponse{Job: job})
		case http.MethodDelete:
			job, err := s.automation.DeleteJob(r.Context(), jobID)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, automation.JobResponse{Job: job})
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
		}
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var (
		job automation.Job
		err error
	)
	switch strings.TrimSpace(parts[1]) {
	case "pause":
		job, err = s.automation.PauseJob(r.Context(), jobID)
	case "resume":
		job, err = s.automation.ResumeJob(r.Context(), jobID)
	case "complete":
		job, err = s.automation.CompleteJob(r.Context(), jobID)
	case "run-now":
		fire, fireErr := s.automation.RunNow(r.Context(), jobID)
		if fireErr != nil {
			writeError(w, fireErr)
			return
		}
		writeJSON(w, http.StatusOK, automation.FireResponse{Fire: fire})
		return
	default:
		writeNotFound(w)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, automation.JobResponse{Job: job})
}
