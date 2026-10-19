package api

import (
	"github.com/Suren878/matrixclaw/internal/memory"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	limit, ok := queryInt(w, r, "limit", 0)
	if !ok {
		return
	}
	entries, err := s.Memory.List(r.Context(), memory.Filter{
		Scope:      memory.Scope(r.URL.Query().Get("scope")),
		WorkingDir: r.URL.Query().Get("working_dir"),
		Pending:    r.URL.Query().Get("pending") == "true",
		Limit:      limit,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.MemoryResponse{Memories: entries})
}

// handleMemoryApprove approves a memory entry a session review suggested.
func (s *Server) handleMemoryApprove(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Memory.Approve(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Memory.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
