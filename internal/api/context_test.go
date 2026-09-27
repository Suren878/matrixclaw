package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestClearEndpointWritesABoundary(t *testing.T) {
	server, st := newAPITestServer(t)
	if err := st.SaveMessage(context.Background(), transcript.Message{ID: "m1", SessionID: "s1", Role: transcript.MessageRoleUser, Content: "hi", CreatedAt: apiTestEpoch}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/sessions/s1/clear", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var response core.MessageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if c := response.Message.Compaction; c == nil || !c.Cleared || c.CoversThroughSeq != 1 {
		t.Fatalf("boundary = %+v", response.Message)
	}
}
