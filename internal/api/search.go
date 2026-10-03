package api

import (
	"net/http"
	"strconv"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
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
