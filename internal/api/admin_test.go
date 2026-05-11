package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestAdminReloadResetsRuntimeStatusClock(t *testing.T) {
	server := New(nil)
	server.startedAt = time.Now().Add(-time.Hour)
	server.hasLastCPU = true
	server.lastCPU = cpuSnapshot{total: 10, idle: 5}
	server.SetAdminReload(func(context.Context) error { return nil })

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/admin/reload", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(reload) error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reload status = %d, want 200", resp.StatusCode)
	}

	statusResp, err := http.Get(httpServer.URL + "/v1/server/status")
	if err != nil {
		t.Fatalf("Get(status) error = %v", err)
	}
	defer statusResp.Body.Close()
	var body core.ServerStatusResponse
	if err := json.NewDecoder(statusResp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode(status) error = %v", err)
	}
	if body.Status.UptimeSeconds > 2 {
		t.Fatalf("uptime = %d, want reset after reload", body.Status.UptimeSeconds)
	}
	if body.Status.CPUKnown {
		t.Fatal("expected CPU sample to reset after reload")
	}
}

func TestAdminRestartCallsConfiguredRestart(t *testing.T) {
	server := New(nil)
	called := false
	var got core.AdminRestartRequest
	server.SetAdminRestart(func(_ context.Context, req core.AdminRestartRequest) error {
		called = true
		got = req
		return nil
	})

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := []byte(`{"notification":{"client":"telegram","external_key":"42","address":{"chat_id":42,"thread_id":7,"message_id":99}}}`)
	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/admin/restart", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(restart) error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restart status = %d, want 200", resp.StatusCode)
	}
	if !called {
		t.Fatal("expected restart callback")
	}
	var address struct {
		ChatID    int64 `json:"chat_id"`
		ThreadID  int64 `json:"thread_id"`
		MessageID int64 `json:"message_id"`
	}
	if got.Notification != nil {
		_ = json.Unmarshal(got.Notification.Address, &address)
	}
	if got.Notification == nil || got.Notification.Client != "telegram" || got.Notification.ExternalKey != "42" || address.ChatID != 42 || address.ThreadID != 7 || address.MessageID != 99 {
		t.Fatalf("restart request = %#v", got)
	}
}
