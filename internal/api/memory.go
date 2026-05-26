package api

import (
	"net/http"
	"strconv"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := s.core.ListMemories(r.Context(), core.MemoryFilter{
		Scope:      core.MemoryScope(r.URL.Query().Get("scope")),
		WorkingDir: r.URL.Query().Get("working_dir"),
		Limit:      limit,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.MemoryResponse{Memories: entries})
}
