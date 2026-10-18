package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	limit, ok := queryInt(w, r, "limit", 0)
	if !ok {
		return
	}
	report, err := s.Core.Search(r.Context(), core.SearchFilter{
		Query:     r.URL.Query().Get("q"),
		SessionID: r.URL.Query().Get("session_id"),
		Limit:     limit,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SearchResponse{Search: report})
}
