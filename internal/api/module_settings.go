package api

import (
	"errors"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/modules"
)

func (s *Server) handleModuleSettings(w http.ResponseWriter, r *http.Request) {
	settings, ok := s.Modules.Set.Settings(r.Context(), r.PathValue("module"))
	if !ok {
		writeErrorMessage(w, http.StatusNotFound, "module has no settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// handleModuleSettingsChange runs a module's change, saves its setup.json
// edit and applies it, and answers with the screen after it.
func (s *Server) handleModuleSettingsChange(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("module")
	module, ok := s.Modules.Set.Configurable(id)
	if !ok {
		writeErrorMessage(w, http.StatusNotFound, "module has no settings")
		return
	}
	var request modules.ChangeRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	change, err := module.Change(r.Context(), request.Path, request.Value)
	switch {
	case errors.Is(err, modules.ErrUnknownSetting):
		writeErrorMessage(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, modules.ErrInvalidSetting):
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	if change.Config != nil {
		if _, err := s.Setup.Update(change.Config); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if (change.Config != nil || change.Reload) && !s.reload(w, r.Context()) {
		return
	}
	settings, _ := s.Modules.Set.Settings(r.Context(), id)
	writeJSON(w, http.StatusOK, modules.ChangeResponse{Settings: settings, Open: change.Open, Message: change.Message})
}
