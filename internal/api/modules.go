package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/modules"
)

func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.modules.Set == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "modules are not configured")
		return
	}
	writeJSON(w, http.StatusOK, modules.StatusResponse{Modules: s.modules.Set.Statuses(r.Context())})
}
