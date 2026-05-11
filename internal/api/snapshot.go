package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	snapshot, err := s.core.ClientSnapshot(r.Context(), r.URL.Query().Get("client"), r.URL.Query().Get("external_key"))
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, core.ClientSnapshotResponse{Snapshot: snapshot})
}
