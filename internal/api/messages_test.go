package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

var apiTestEpoch = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

func newAPITestServer(t *testing.T) (*Server, *store.SQLiteStore) {
	t.Helper()
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	session := core.Session{ID: "s1", Title: "s1", Kind: core.SessionKindAssistant, Status: core.SessionStatusActive, CreatedAt: apiTestEpoch, UpdatedAt: apiTestEpoch}
	if err := st.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return New(core.New(st)), st
}

func TestListMessagesAfterSeq(t *testing.T) {
	server, st := newAPITestServer(t)
	for _, id := range []string{"m1", "m2", "m3"} {
		message := transcript.Message{ID: id, SessionID: "s1", Role: transcript.MessageRoleUser, Content: id, CreatedAt: apiTestEpoch}
		if _, err := st.AppendMessage(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query      string
		wantStatus int
		wantIDs    string
	}{
		{"session_id=s1&after_seq=1", http.StatusOK, "m2,m3"},
		{"session_id=s1&after_seq=1&limit=1", http.StatusOK, "m2"},
		{"session_id=s1&after_seq=3", http.StatusOK, ""},
		{"session_id=s1&limit=2", http.StatusOK, "m2,m3"},
		{"session_id=s1&after_seq=x", http.StatusBadRequest, ""},
	} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/messages?"+tc.query, nil))
		if recorder.Code != tc.wantStatus {
			t.Fatalf("%s: status=%d body=%s", tc.query, recorder.Code, recorder.Body.String())
		}
		if tc.wantStatus != http.StatusOK {
			continue
		}
		var response core.MessagesResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(response.Messages))
		for _, message := range response.Messages {
			ids = append(ids, message.ID)
		}
		if got := strings.Join(ids, ","); got != tc.wantIDs {
			t.Errorf("%s: ids=%q, want %q", tc.query, got, tc.wantIDs)
		}
	}
}
