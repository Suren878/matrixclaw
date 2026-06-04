package localruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
)

var supertonicServerProcesses = &supertonicServerProcessManager{
	processes: map[string]*supertonicServerProcess{},
}

type supertonicServerProcessManager struct {
	mu        sync.Mutex
	processes map[string]*supertonicServerProcess
}

type supertonicServerProcess struct {
	cmd      *exec.Cmd
	done     chan struct{}
	endpoint string
}

func (r *Runtime) startSupertonicServerProcess(ctx context.Context, provider setup.VoiceProviderOption) error {
	if provider.ID != "supertonic" {
		return fmt.Errorf("local voice provider %q cannot be started", provider.ID)
	}
	binary, err := r.VoiceBinaryPath(provider)
	if err != nil {
		return err
	}
	endpoint := r.supertonicServerEndpoint(provider)
	host, port := supertonicServerHostPort(endpoint)
	key := r.supertonicServerProcessKey(provider)

	supertonicServerProcesses.mu.Lock()
	defer supertonicServerProcesses.mu.Unlock()
	if process := supertonicServerProcesses.processes[key]; process != nil && process.running() && process.endpoint == endpoint {
		return nil
	}
	if process := supertonicServerProcesses.processes[key]; process != nil {
		_ = process.stop()
		delete(supertonicServerProcesses.processes, key)
	}

	cmd := exec.Command(binary, "serve", "--host", host, "--port", port, "--model", "supertonic-3", "--log-level", "warning")
	cmd.Env = r.supertonicEnv(provider)
	cmd.Stdout = io.Discard
	logFile, err := os.OpenFile(filepath.Join(r.runtimeDir(), "supertonic-server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		defer logFile.Close()
		cmd.Stderr = logFile
	} else {
		cmd.Stderr = io.Discard
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	process := &supertonicServerProcess{
		cmd:      cmd,
		done:     make(chan struct{}),
		endpoint: endpoint,
	}
	supertonicServerProcesses.processes[key] = process
	safego.Go("localruntime.supertonicWait", func() {
		defer close(process.done)
		_ = cmd.Wait()
	})
	if err := r.waitSupertonicServer(ctx, endpoint, process, 90*time.Second); err != nil {
		delete(supertonicServerProcesses.processes, key)
		_ = process.stop()
		return err
	}
	return nil
}

func (r *Runtime) stopSupertonicServerProcess(provider setup.VoiceProviderOption) error {
	key := r.supertonicServerProcessKey(provider)
	supertonicServerProcesses.mu.Lock()
	process := supertonicServerProcesses.processes[key]
	delete(supertonicServerProcesses.processes, key)
	supertonicServerProcesses.mu.Unlock()
	if process == nil {
		return nil
	}
	return process.stop()
}

func (r *Runtime) supertonicServerProcessRunning(provider setup.VoiceProviderOption) bool {
	key := r.supertonicServerProcessKey(provider)
	supertonicServerProcesses.mu.Lock()
	process := supertonicServerProcesses.processes[key]
	supertonicServerProcesses.mu.Unlock()
	return process != nil && process.running()
}

func (p *supertonicServerProcess) running() bool {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *supertonicServerProcess) stop() error {
	if p == nil {
		return nil
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
	}
	return nil
}

func (r *Runtime) supertonicServerTextToSpeech(ctx context.Context, provider setup.VoiceProviderOption, text string) ([]byte, error) {
	if !r.supertonicServerProcessRunning(provider) {
		if err := r.startSupertonicServerProcess(ctx, provider); err != nil {
			return nil, err
		}
	}
	voiceID := strings.ToUpper(strings.TrimSpace(provider.Config.VoiceID))
	if voiceID == "" {
		voiceID = "M1"
	}
	payload := map[string]any{
		"text":            normalizeTTSInputText(text),
		"voice":           voiceID,
		"response_format": "wav",
	}
	if language := supertonicLanguageArg(provider.Config.Language); language != "" {
		payload["lang"] = language
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(r.supertonicServerEndpoint(provider), "/") + "/v1/tts"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := r.httpClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(content))
		if message == "" {
			message = response.Status
		}
		return nil, fmt.Errorf("supertonic server failed: %s", message)
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("supertonic server returned empty audio")
	}
	return content, nil
}

func (r *Runtime) waitSupertonicServer(ctx context.Context, endpoint string, process *supertonicServerProcess, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if process != nil && !process.running() {
			return fmt.Errorf("supertonic server exited before it was ready")
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/health", nil)
		if err != nil {
			return err
		}
		response, err := r.httpClient().Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("supertonic server did not start: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *Runtime) supertonicServerProcessKey(provider setup.VoiceProviderOption) string {
	return filepath.Join(r.rootDir(), strings.TrimSpace(provider.ID))
}

func (r *Runtime) supertonicServerEndpoint(provider setup.VoiceProviderOption) string {
	if endpoint := strings.TrimSpace(provider.Config.Endpoint); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	return "http://127.0.0.1:7788"
}

func supertonicServerHostPort(endpoint string) (string, string) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "127.0.0.1", "7788"
	}
	host := parsed.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port := parsed.Port()
	if port == "" {
		port = "7788"
	}
	return host, port
}
