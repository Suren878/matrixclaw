package openaicodex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
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
	for _, entry := range entries {
		if name := entry.Name(); name != "openai-codex.json" && name != "openai-codex.json.lock" {
			t.Fatalf("auth dir holds %s, want only the store and its lock", name)
		}
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
		if !errors.As(err, &apiErr) || apiErr.Status != status || apiErr.RetryAfter != 2*time.Second || !isRetryable(err) {
			t.Fatalf("%d: err=%v", status, err)
		}
	}
}

func TestRefreshOutlivesACanceledRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openai-codex.json")
	t.Setenv("MATRIXCLAW_CODEX_AUTH_FILE", path)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	raw, _ := json.Marshal(tokenStore{Tokens: map[string]string{"access_token": testJWT(time.Now().Add(time.Minute), "old"), "refresh_token": "r1"}})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		time.Sleep(50 * time.Millisecond)
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"r2"}`, testJWT(time.Now().Add(time.Hour), "new"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)

	_, _ = ResolveCredentials(ctx, &http.Client{Transport: redirectTransport{target: target}}, "")
	creds, ok, err := readMatrixclawCredentials()
	if err != nil || !ok || creds.RefreshToken != "r2" {
		t.Fatalf("store = %+v ok=%v err=%v; want the rotated refresh token saved", creds, ok, err)
	}
}

// writeStore seeds the auth store at a fresh temp path.
func writeStore(t *testing.T, access string, refresh string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "openai-codex.json")
	t.Setenv("MATRIXCLAW_CODEX_AUTH_FILE", path)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	raw, _ := json.Marshal(tokenStore{Tokens: map[string]string{"access_token": access, "refresh_token": refresh}})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// tokenServer rotates refresh token r1 to r2 once and rejects a reused one.
func tokenServer(t *testing.T, fresh string, delay time.Duration) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "r1" || refreshes.Add(1) > 1 {
			http.Error(w, `{"error":{"message":"refresh token reused","code":"invalid_grant"}}`, http.StatusBadRequest)
			return
		}
		time.Sleep(delay)
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"r2"}`, fresh)
	}))
	t.Cleanup(server.Close)
	return server, &refreshes
}

// authRoute sends OAuth token requests to the token server, the rest as is.
type authRoute struct{ token *url.URL }

func (t authRoute) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "auth.openai.com" {
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = t.token.Scheme, t.token.Host
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestUnauthorizedReplyRefreshesOnceAndRetries(t *testing.T) {
	old, fresh := testJWT(time.Now().Add(time.Hour), "old"), testJWT(time.Now().Add(time.Hour), "new")
	writeStore(t, old, "r1")
	tokens, refreshes := tokenServer(t, fresh, 0)
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+fresh {
			http.Error(w, `{"error":{"message":"token revoked"}}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer backend.Close()
	target, _ := url.Parse(tokens.URL)
	runtime, err := New(context.Background(), providers.RuntimeConfig{BaseURL: backend.URL, Model: "gpt-5.4", HTTPClient: &http.Client{Transport: authRoute{token: target}}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hi"}}})
	if err != nil || response.Text != "ok" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if refreshes.Load() != 1 || calls.Load() != 2 {
		t.Fatalf("refreshes=%d calls=%d, want 1 and 2", refreshes.Load(), calls.Load())
	}
}

func TestUnauthorizedAfterRefreshAsksForSignIn(t *testing.T) {
	writeStore(t, testJWT(time.Now().Add(time.Hour), "old"), "r1")
	tokens, refreshes := tokenServer(t, testJWT(time.Now().Add(time.Hour), "new"), 0)
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":{"message":"account deactivated"}}`, http.StatusUnauthorized)
	}))
	defer backend.Close()
	target, _ := url.Parse(tokens.URL)
	runtime, _ := New(context.Background(), providers.RuntimeConfig{BaseURL: backend.URL, Model: "gpt-5.4", HTTPClient: &http.Client{Transport: authRoute{token: target}}})
	_, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hi"}}})
	if kind, _ := providers.ClassifyError(err); !errors.Is(err, providers.ErrReauthRequired) || kind != providers.ErrorAuth {
		t.Fatalf("err=%v kind=%s, want ErrReauthRequired", err, kind)
	}
	if refreshes.Load() != 1 || calls.Load() != 2 {
		t.Fatalf("refreshes=%d calls=%d, want 1 and 2", refreshes.Load(), calls.Load())
	}
}

func TestRejectedRefreshTokenAsksForSignIn(t *testing.T) {
	writeStore(t, testJWT(time.Now().Add(time.Minute), "old"), "spent")
	tokens, _ := tokenServer(t, "", 0)
	target, _ := url.Parse(tokens.URL)
	_, err := ResolveCredentials(context.Background(), &http.Client{Transport: authRoute{token: target}}, "")
	if !errors.Is(err, providers.ErrReauthRequired) {
		t.Fatalf("err=%v, want ErrReauthRequired", err)
	}
}

const refreshHelperEnv = "MATRIXCLAW_CODEX_REFRESH_HELPER"

// TestRefreshHelperProcess is the other process of the cross-process test.
func TestRefreshHelperProcess(t *testing.T) {
	tokenURL := os.Getenv(refreshHelperEnv)
	if tokenURL == "" {
		t.Skip("helper process only")
	}
	target, _ := url.Parse(tokenURL)
	if _, err := ResolveCredentials(context.Background(), &http.Client{Transport: authRoute{token: target}}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshIsSerialisedAcrossProcesses(t *testing.T) {
	path := writeStore(t, testJWT(time.Now().Add(time.Minute), "old"), "r1")
	fresh := testJWT(time.Now().Add(time.Hour), "new")
	tokens, refreshes := tokenServer(t, fresh, 300*time.Millisecond)
	procs := make([]*exec.Cmd, 2)
	outputs := make([]*bytes.Buffer, 2)
	for i := range procs {
		outputs[i] = &bytes.Buffer{}
		procs[i] = exec.Command(os.Args[0], "-test.run=^TestRefreshHelperProcess$", "-test.count=1")
		procs[i].Env = append(os.Environ(), refreshHelperEnv+"="+tokens.URL, "MATRIXCLAW_CODEX_AUTH_FILE="+path)
		procs[i].Stdout, procs[i].Stderr = outputs[i], outputs[i]
		if err := procs[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, proc := range procs {
		if err := proc.Wait(); err != nil {
			t.Fatalf("process %d: %v\n%s", i, err, outputs[i])
		}
	}
	if got := refreshes.Load(); got != 1 {
		t.Fatalf("refreshes=%d, want 1", got)
	}
	creds, ok, err := readMatrixclawCredentials()
	if err != nil || !ok || creds.AccessToken != fresh || creds.RefreshToken != "r2" {
		t.Fatalf("store=%+v ok=%v err=%v", creds, ok, err)
	}
}
