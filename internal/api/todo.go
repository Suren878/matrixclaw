package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionTodo(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.core.SessionTodo(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionTodoResponse{Todo: list})
	case http.MethodDelete:
		list, err := s.core.ClearSessionTodo(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionTodoResponse{Todo: list})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}
