package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (m *systemdUserDaemonManager) inspectViaHealth(ctx context.Context, cfg Config, warning string) (DaemonSummary, error) {
	summary := daemonConfiguredSummary(cfg)
	if m.healthCheck(ctx, cfg.Daemon.HTTPAddr) == nil {
		summary.RuntimeStatus = "Running"
		summary.Running = true
		summary.Warning = strings.TrimSpace(warning)
		return summary, nil
	}
	summary.RuntimeStatus = "Unavailable"
	summary.Warning = strings.TrimSpace(warning)
	if summary.Warning == "" {
		summary.Warning = "daemon is not reachable"
	}
	return summary, nil
}

func (m *systemdUserDaemonManager) waitForHealth(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = m.healthCheck(ctx, addr)
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return lastErr
}

func (m *systemdUserDaemonManager) healthCheck(ctx context.Context, addr string) error {
	client := m.httpClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, daemonHealthURL(addr), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	if !payload.OK {
		return errors.New("daemon health returned not ok")
	}
	return nil
}

func (m *systemdUserDaemonManager) reloadLiveDaemon(ctx context.Context, cfg Config) error {
	client := m.httpClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, daemonReloadURL(cfg.Daemon.HTTPAddr), nil)
	if err != nil {
		return err
	}
	if token := strings.TrimSpace(cfg.Daemon.APIToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("reload status %d", resp.StatusCode)
	}
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	if !payload.OK {
		return errors.New("daemon reload returned not ok")
	}
	return nil
}

func daemonHealthURL(addr string) string {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "http://127.0.0.1:8080/v1/health"
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return strings.TrimRight(trimmed, "/") + "/v1/health"
	}
	return "http://" + strings.TrimRight(trimmed, "/") + "/v1/health"
}

func daemonReloadURL(addr string) string {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "http://127.0.0.1:8080/v1/admin/reload"
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return strings.TrimRight(trimmed, "/") + "/v1/admin/reload"
	}
	return "http://" + strings.TrimRight(trimmed, "/") + "/v1/admin/reload"
}
