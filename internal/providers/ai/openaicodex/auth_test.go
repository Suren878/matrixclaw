package openaicodex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func testJWT(exp time.Time, tag string) string {
	claims, _ := json.Marshal(map[string]any{"exp": exp.Unix(), "tag": tag})
	return "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
}

type redirectTransport struct{ target *url.URL }

func (t redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = t.target.Scheme, t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestConcurrentResolveRefreshesOnceAndRewritesTheStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openai-codex.json")
	t.Setenv("MATRIXCLAW_CODEX_AUTH_FILE", path)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	stale := tokenStore{Tokens: map[string]string{"access_token": testJWT(time.Now().Add(time.Minute), "old"), "refresh_token": "r1"}}
	raw, _ := json.Marshal(stale)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := testJWT(time.Now().Add(time.Hour), "new")
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "r1" {
			http.Error(w, `{"error":{"message":"refresh token reused"}}`, http.StatusUnauthorized)
			return
		}
		refreshes.Add(1)
		time.Sleep(20 * time.Millisecond)
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"r2"}`, fresh)
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: redirectTransport{target: target}}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			creds, err := ResolveCredentials(context.Background(), client, "")
			if err == nil && creds.AccessToken != fresh {
				err = fmt.Errorf("got stale access token")
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := refreshes.Load(); got != 1 {
		t.Fatalf("refreshes=%d, want 1", got)
	}
	creds, ok, err := readMatrixclawCredentials()
	if err != nil || !ok || creds.AccessToken != fresh || creds.RefreshToken != "r2" {
		t.Fatalf("store=%+v ok=%v err=%v", creds, ok, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("auth dir holds %d entries, want only the store", len(entries))
	}
}

func TestGenerateReportsRateLimitsAsRetryable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openai-codex.json")
	t.Setenv("MATRIXCLAW_CODEX_AUTH_FILE", path)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	raw, _ := json.Marshal(tokenStore{Tokens: map[string]string{"access_token": testJWT(time.Now().Add(time.Hour), "a"), "refresh_token": "r"}})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "2")
			http.Error(w, `{"error":{"message":"slow down"}}`, status)
		}))
		runtime, err := New(context.Background(), providers.RuntimeConfig{BaseURL: server.URL, Model: "gpt-5.4"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hi"}}})
		server.Close()
		var apiErr *providers.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != status || apiErr.RetryAfter != 2*time.Second || !providers.IsRetryableGenerationError(err) {
			t.Fatalf("%d: err=%v", status, err)
		}
	}
}
