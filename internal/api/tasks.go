package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.Core.ListSessionTasks(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionTasksResponse{Tasks: tasks})
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	detail, err := s.Core.TaskDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	task, err := s.Core.CancelTask(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.TaskResponse{Task: task})
}
