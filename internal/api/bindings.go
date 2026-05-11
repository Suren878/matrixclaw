package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleCurrentBinding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	client := r.URL.Query().Get("client")
	externalKey := r.URL.Query().Get("external_key")

	binding, err := s.core.CurrentBinding(r.Context(), client, externalKey)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, core.ClientBindingResponse{Binding: binding})
}

func (s *Server) handleUseBinding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	var input core.UseBindingInput
	if !decodeJSONBody(w, r, &input) {
		return
	}

	binding, err := s.core.UseBinding(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, core.ClientBindingResponse{Binding: binding})
}
