package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestListModelsEscapesPageToken(t *testing.T) {
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		token := r.URL.Query().Get("pageToken")
		tokens = append(tokens, token)
		if token == "" {
			_, _ = w.Write([]byte(`{"models":[{"name":"models/a","supportedGenerationMethods":["generateContent"]}],"nextPageToken":"p+q/r=&s"}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/b","supportedGenerationMethods":["generateContent"]}]}`))
	}))
	defer server.Close()
	models, err := ListModels(context.Background(), providers.RuntimeConfig{ProviderID: "gemini", APIKey: "k", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || len(tokens) != 2 || tokens[1] != "p+q/r=&s" {
		t.Fatalf("models=%v tokens=%q", models, tokens)
	}
}
