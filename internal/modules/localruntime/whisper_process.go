package localruntime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
)

var whisperServerProcesses = &whisperServerProcessManager{
	processes: map[string]*whisperServerProcess{},
}

type whisperServerProcessManager struct {
	mu        sync.Mutex
	processes map[string]*whisperServerProcess
}

type whisperServerProcess struct {
	cmd       *exec.Cmd
	done      chan struct{}
	modelPath string
	endpoint  string
}

func (r *Runtime) startWhisperServerProcess(ctx context.Context, moduleID string, provider setup.VoiceProviderOption) error {
	if provider.ID != "whispercpp" {
		return fmt.Errorf("local voice provider %q cannot be started", provider.ID)
	}
	installed, _ := r.VoiceModelInstalled(moduleID, provider)
	if !installed {
		return fmt.Errorf("whisper.cpp model is not installed")
	}
	modelPath := r.VoiceModelPath(moduleID, provider)
	if strings.TrimSpace(modelPath) == "" {
		return fmt.Errorf("whisper.cpp model is not selected")
	}
	binary, err := r.WhisperServerPath(provider)
	if err != nil {
		return err
	}
	endpoint := r.whisperServerEndpoint(provider)
	host, port := whisperServerHostPort(endpoint)
	key := r.whisperServerProcessKey(provider)

	whisperServerProcesses.mu.Lock()
	defer whisperServerProcesses.mu.Unlock()
	if process := whisperServerProcesses.processes[key]; process != nil && process.running() && process.modelPath == modelPath && process.endpoint == endpoint {
		return nil
	}
	if process := whisperServerProcesses.processes[key]; process != nil {
		_ = process.stop()
		delete(whisperServerProcesses.processes, key)
	}

	args := []string{"--model", modelPath, "--host", host, "--port", port, "--convert"}
	if language := whisperLanguageArg(provider.Config.Language); language != "" {
		args = append(args, "--language", language)
	}
	if provider.Config.Threads > 0 {
		args = append(args, "--threads", strconv.Itoa(provider.Config.Threads))
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdout = io.Discard
	logFile, err := os.OpenFile(filepath.Join(r.runtimeDir(), "whisper-server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		defer logFile.Close()
		cmd.Stderr = logFile
	} else {
		cmd.Stderr = io.Discard
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	process := &whisperServerProcess{
		cmd:       cmd,
		done:      make(chan struct{}),
		modelPath: modelPath,
		endpoint:  endpoint,
	}
	whisperServerProcesses.processes[key] = process
	safego.Go("localruntime.whisperWait", func() {
		defer close(process.done)
		_ = cmd.Wait()
	})
	if err := r.waitWhisperServer(ctx, endpoint, process, 60*time.Second); err != nil {
		delete(whisperServerProcesses.processes, key)
		_ = process.stop()
		return err
	}
	return nil
}

func (r *Runtime) stopWhisperServerProcess(provider setup.VoiceProviderOption) error {
	key := r.whisperServerProcessKey(provider)
	whisperServerProcesses.mu.Lock()
	process := whisperServerProcesses.processes[key]
	delete(whisperServerProcesses.processes, key)
	whisperServerProcesses.mu.Unlock()
	if process == nil {
		return nil
	}
	return process.stop()
}

func (r *Runtime) whisperServerProcessRunning(provider setup.VoiceProviderOption) bool {
	key := r.whisperServerProcessKey(provider)
	whisperServerProcesses.mu.Lock()
	process := whisperServerProcesses.processes[key]
	whisperServerProcesses.mu.Unlock()
	return process != nil && process.running()
}

func (p *whisperServerProcess) running() bool {
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

func (p *whisperServerProcess) stop() error {
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

func (r *Runtime) waitWhisperServer(ctx context.Context, endpoint string, process *whisperServerProcess, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if process != nil && !process.running() {
			return fmt.Errorf("whisper.cpp server exited before it was ready")
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/"), nil)
		if err != nil {
			return err
		}
		response, err := r.httpClient().Do(request)
		if err == nil {
			_ = response.Body.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("whisper.cpp server did not start: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *Runtime) whisperServerProcessKey(provider setup.VoiceProviderOption) string {
	return filepath.Join(r.rootDir(), strings.TrimSpace(provider.ID))
}

func (r *Runtime) whisperServerEndpoint(provider setup.VoiceProviderOption) string {
	if endpoint := strings.TrimSpace(provider.Config.Endpoint); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	return "http://127.0.0.1:5011"
}

func whisperServerHostPort(endpoint string) (string, string) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "127.0.0.1", "5011"
	}
	host := parsed.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port := parsed.Port()
	if port == "" {
		port = "5011"
	}
	return host, port
}
