package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionTasks(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	tasks, err := s.core.ListSessionTasks(r.Context(), sessionID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionTasksResponse{Tasks: tasks})
}

// handleTaskByID serves GET /v1/tasks/{id} and POST /v1/tasks/{id}/cancel.
func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	taskID, action, _ := strings.Cut(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/"), "/")
	switch {
	case taskID == "":
		writeNotFound(w)
	case action == "":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		detail, err := s.core.TaskDetail(r.Context(), taskID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	case action == "cancel":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		task, err := s.core.CancelTask(r.Context(), taskID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.TaskResponse{Task: task})
	default:
		writeNotFound(w)
	}
}
