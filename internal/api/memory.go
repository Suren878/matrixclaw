package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	limit, ok := queryInt(w, r, "limit", 0)
	if !ok {
		return
	}
	entries, err := s.Core.ListMemories(r.Context(), core.MemoryFilter{
		Scope:      core.MemoryScope(r.URL.Query().Get("scope")),
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
	if _, err := s.Core.ApproveMemory(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Core.DeleteMemory(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
