package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSessionTodoEndpointReadsAndClearsTheList(t *testing.T) {
	server, st := newAPITestServer(t)
	unwritten := httptest.NewRecorder()
	server.Handler().ServeHTTP(unwritten, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/todo", nil))
	if !strings.Contains(unwritten.Body.String(), `"items":[]`) {
		t.Fatalf("GET an unwritten todo: %s", unwritten.Body.String())
	}
	if err := st.SaveSessionTodo(context.Background(), todo.List{SessionID: "s1", Items: []todo.Item{{Content: "Run the tests", Status: todo.Pending}}, UpdatedRunID: "run_1", UpdatedAt: apiTestEpoch}); err != nil {
		t.Fatal(err)
	}
	decode := func(recorder *httptest.ResponseRecorder) todo.List {
		t.Helper()
		var response core.SessionTodoResponse
		if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &response) != nil {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		return response.Todo
	}

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/todo", nil))
	if list := decode(get); len(list.Items) != 1 || list.Items[0].Content != "Run the tests" || list.UpdatedRunID != "run_1" {
		t.Fatalf("GET todo = %+v", list)
	}

	del := httptest.NewRecorder()
	server.Handler().ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/v1/sessions/s1/todo", nil))
	if list := decode(del); len(list.Items) != 0 || !strings.Contains(del.Body.String(), `"items":[]`) {
		t.Fatalf("DELETE todo = %+v", list)
	}
	if stored, err := st.GetSessionTodo(context.Background(), "s1"); err != nil || len(stored.Items) != 0 {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}

	missing := httptest.NewRecorder()
	server.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/v1/sessions/nope/todo", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("GET todo of a missing session: status=%d", missing.Code)
	}
}
