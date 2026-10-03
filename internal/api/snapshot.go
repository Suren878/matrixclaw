package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.Core.ClientSnapshot(r.Context(), r.URL.Query().Get("client"), r.URL.Query().Get("external_key"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.ClientSnapshotResponse{Snapshot: snapshot})
}
