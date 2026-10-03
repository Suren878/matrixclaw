package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleTelephonyModule(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getTelephonyModule(w, r)
	case http.MethodPatch:
		s.updateTelephonyModule(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (s *Server) getTelephonyModule(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, setup.TelephonyModuleResponse{Module: s.modules.Telephony.Descriptor(r.Context())})
}

func (s *Server) updateTelephonyModule(w http.ResponseWriter, r *http.Request) {
	var update setup.TelephonyModuleUpdate
	if !decodeJSONBody(w, r, &update) {
		return
	}
	if _, err := s.setup.UpdateTelephonyModule(update); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reload(w, r.Context()) {
		return
	}
	writeJSON(w, http.StatusOK, setup.TelephonyModuleResponse{Module: s.modules.Telephony.Descriptor(r.Context())})
}
