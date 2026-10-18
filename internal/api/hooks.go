package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionHooks(w http.ResponseWriter, r *http.Request) {
	listing, err := s.Core.SessionHooks(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (s *Server) handleSessionHooksTrust(w http.ResponseWriter, r *http.Request) {
	var request core.TrustHooksRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	listing, err := s.Core.TrustSessionHooks(r.Context(), r.PathValue("id"), request.SHA256)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}
