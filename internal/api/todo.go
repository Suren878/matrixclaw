package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionTodo(w http.ResponseWriter, r *http.Request) {
	list, err := s.Core.SessionTodo(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionTodoResponse{Todo: list})
}

func (s *Server) handleSessionTodoClear(w http.ResponseWriter, r *http.Request) {
	list, err := s.Core.ClearSessionTodo(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionTodoResponse{Todo: list})
}
