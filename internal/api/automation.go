package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/automation"
)

func (s *Server) handleAutomationJobs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	jobs, err := s.Automation.ListJobs(r.Context(), automation.JobFilter{
		Status:    automation.JobStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		SessionID: strings.TrimSpace(r.URL.Query().Get("session_id")),
		Limit:     limit,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, automation.JobsResponse{Jobs: jobs})
}

func (s *Server) handleAutomationJobCreate(w http.ResponseWriter, r *http.Request) {
	var request automation.CreateJobRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	input, err := request.CreateInput()
	if err != nil {
		writeError(w, err)
		return
	}
	job, err := s.Automation.CreateJob(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, automation.JobResponse{Job: job})
}

func (s *Server) handleAutomationJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.Automation.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, automation.JobResponse{Job: job})
}

func (s *Server) handleAutomationJobDelete(w http.ResponseWriter, r *http.Request) {
	job, err := s.Automation.DeleteJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, automation.JobResponse{Job: job})
}

func (s *Server) handleAutomationJobAction(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	var (
		job automation.Job
		err error
	)
	switch r.PathValue("action") {
	case "pause":
		job, err = s.Automation.PauseJob(r.Context(), jobID)
	case "resume":
		job, err = s.Automation.ResumeJob(r.Context(), jobID)
	case "complete":
		job, err = s.Automation.CompleteJob(r.Context(), jobID)
	case "run-now":
		fire, fireErr := s.Automation.RunNow(r.Context(), jobID)
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
