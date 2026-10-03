package openaicompat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestListModelsProbesContextWindowOnLoopbackIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"box-model"}]}`))
		case "/api/show":
			_, _ = w.Write([]byte(`{"parameters":"num_ctx 32768"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if _, err := ListModels(context.Background(), Config{ProviderID: "home-box", APIKey: "k", BaseURL: server.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	if got := providers.ResolveContextWindowTokens("home-box", providers.TypeOpenAICompat, "box-model"); got != 32768 {
		t.Fatalf("context window=%d, want 32768", got)
	}
}
