package daemonclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestCompactSessionOutlastsAShortClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(core.SessionCompactResponse{})
	}))
	t.Cleanup(server.Close)
	client := New(server.URL, "telegram", "42")
	client.HTTPClient = &http.Client{Timeout: 50 * time.Millisecond}

	if _, err := client.CompactSession(context.Background(), "s1"); err != nil {
		t.Fatalf("compact with a 50ms client: %v", err)
	}
}
