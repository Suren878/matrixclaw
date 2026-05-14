package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckerDetectsNewerRelease(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Suren878/matrixclaw/releases/latest" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.6","html_url":"https://example.test/release"}`))
	}))
	defer server.Close()

	update, ok, err := (Checker{BaseURL: server.URL, HTTPClient: server.Client()}).Check(context.Background(), "0.1.5")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !ok {
		t.Fatal("Check() ok = false, want true")
	}
	if update.Current != "v0.1.5" || update.Latest != "v0.1.6" {
		t.Fatalf("update = %#v, want v0.1.5 -> v0.1.6", update)
	}
}

func TestCheckerIgnoresCurrentRelease(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.5"}`))
	}))
	defer server.Close()

	_, ok, err := (Checker{BaseURL: server.URL, HTTPClient: server.Client()}).Check(context.Background(), "v0.1.5")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if ok {
		t.Fatal("Check() ok = true, want false")
	}
}

func TestCheckerSkipsDevVersion(t *testing.T) {
	t.Parallel()

	_, ok, err := (Checker{}).Check(context.Background(), "dev")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if ok {
		t.Fatal("Check() ok = true, want false")
	}
}
