package api

import "net/http"

func (s *Server) handleSessionCheckpoints(w http.ResponseWriter, r *http.Request) {
	list, err := s.Core.FileCheckpoints(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleSessionCheckpointRestore rewrites the session's files: owner only.
func (s *Server) handleSessionCheckpointRestore(w http.ResponseWriter, r *http.Request) {
	restored, err := s.Core.RestoreFileCheckpoint(r.Context(), r.PathValue("id"), r.PathValue("checkpoint"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, restored)
}
