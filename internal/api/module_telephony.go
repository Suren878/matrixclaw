package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleTelephonyModule(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, setup.TelephonyModuleResponse{Module: s.Modules.Telephony.Descriptor(r.Context())})
}

func (s *Server) handleTelephonyModuleUpdate(w http.ResponseWriter, r *http.Request) {
	var update setup.TelephonyModuleUpdate
	if !decodeJSON(w, r, &update) {
		return
	}
	if _, err := s.Setup.UpdateTelephonyModule(update); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reload(w, r.Context()) {
		return
	}
	writeJSON(w, http.StatusOK, setup.TelephonyModuleResponse{Module: s.Modules.Telephony.Descriptor(r.Context())})
}
