package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestModelsCapsRemoteListingWithTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)

	original := modelsDiscoveryTimeout
	modelsDiscoveryTimeout = 50 * time.Millisecond
	defer func() { modelsDiscoveryTimeout = original }()

	start := time.Now()
	_, err := Models(context.Background(), ModelDiscoveryInput{
		ID:      "test-provider",
		Type:    providers.TypeOpenAICompat,
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from a server that never responds")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Models took %s, want it capped near the injected timeout", elapsed)
	}
}
