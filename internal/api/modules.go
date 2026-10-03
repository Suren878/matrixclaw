package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/modules"
)

func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, modules.StatusResponse{Modules: s.Modules.Set.Statuses(r.Context())})
}
