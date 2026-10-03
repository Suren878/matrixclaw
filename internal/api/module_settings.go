package api

import (
	"errors"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules"
)

// secretNotSet is what a secret item shows while no key is set.
const secretNotSet = "Not set"

func (s *Server) handleModuleSettings(w http.ResponseWriter, r *http.Request) {
	settings, ok := s.Modules.Set.Settings(r.Context(), r.PathValue("module"))
	if !ok {
		writeErrorMessage(w, http.StatusNotFound, "module has no settings")
		return
	}
	if roleOf(r) != core.RoleOwner {
		settings.Items = hideSecretPreviews(settings.Items)
	}
	writeJSON(w, http.StatusOK, settings)
}

// hideSecretPreviews replaces masked key previews with whether a key is set,
// for callers who may only look.
func hideSecretPreviews(items []modules.Item) []modules.Item {
	out := make([]modules.Item, len(items))
	for i, item := range items {
		if item.Kind == modules.ItemSecret && item.Display != "" && item.Display != secretNotSet {
			item.Display = "Set"
		}
		item.Items = hideSecretPreviews(item.Items)
		out[i] = item
	}
	return out
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
