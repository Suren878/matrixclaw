package api

import (
	"net/http"
	"strconv"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	report, err := s.core.Search(r.Context(), core.SearchFilter{
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
