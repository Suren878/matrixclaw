package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleCurrentBinding(w http.ResponseWriter, r *http.Request) {
	binding, err := s.Core.CurrentBinding(r.Context(), r.URL.Query().Get("client"), r.URL.Query().Get("external_key"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.ClientBindingResponse{Binding: binding})
}

func (s *Server) handleUseBinding(w http.ResponseWriter, r *http.Request) {
	var input core.UseBindingInput
	if !decodeJSON(w, r, &input) {
		return
	}
	binding, err := s.Core.UseBinding(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.ClientBindingResponse{Binding: binding})
}
