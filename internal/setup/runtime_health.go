package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
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

// waitForDown returns once the daemon no longer answers its health check.
func (m *systemdUserDaemonManager) waitForDown(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for m.healthCheck(ctx, addr) == nil {
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon still answers on %s after %s", addr, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil
}

func (m *systemdUserDaemonManager) ensureDaemonPortAvailable(ctx context.Context, addr string) error {
	if m.healthCheck(ctx, addr) == nil {
		return nil
	}
	if m.portAvailable != nil {
		return m.portAvailable(addr)
	}
	listenAddr, ok := daemonTCPListenAddr(addr)
	if !ok {
		return nil
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err == nil {
		_ = ln.Close()
		return nil
	}
	return fmt.Errorf("daemon API address %s is already in use; stop the process using that port or choose another address in setup", listenAddr)
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
	defer func() { _ = resp.Body.Close() }()
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
	return m.adminPost(ctx, cfg, "reload")
}

// adminPost calls the running daemon's owner-only /v1/admin/<action>.
func (m *systemdUserDaemonManager) adminPost(ctx context.Context, cfg Config, action string) error {
	client := m.httpClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, daemonAdminURL(cfg.Daemon.HTTPAddr, action), nil)
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s status %d", action, resp.StatusCode)
	}
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	if !payload.OK {
		return fmt.Errorf("daemon %s returned not ok", action)
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

func daemonTCPListenAddr(addr string) (string, bool) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		trimmed = "127.0.0.1:8080"
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", false
		}
		trimmed = parsed.Host
	}
	host, port, err := net.SplitHostPort(trimmed)
	if err != nil || strings.TrimSpace(port) == "" {
		return "", false
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), true
}

func firstAvailableLoopbackHTTPAddr() string {
	for port := 8080; port <= 8099; port++ {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			_ = ln.Close()
			return "127.0.0.1:" + strconv.Itoa(port)
		}
	}
	return "127.0.0.1:8080"
}

func daemonAdminURL(addr string, action string) string {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		trimmed = "127.0.0.1:8080"
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		trimmed = "http://" + trimmed
	}
	return strings.TrimRight(trimmed, "/") + "/v1/admin/" + action
}
