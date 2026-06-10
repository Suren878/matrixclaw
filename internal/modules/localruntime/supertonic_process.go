package localruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

var supertonicServerProcesses = &localProcessManager[*supertonicServerProcess]{
	processes: map[string]*supertonicServerProcess{},
}

type supertonicServerProcess struct {
	*managedProcess
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
	key := r.localVoiceProcessKey(provider)

	supertonicServerProcesses.mu.Lock()
	defer supertonicServerProcesses.mu.Unlock()
	if process := supertonicServerProcesses.processes[key]; process != nil && process.running() && process.endpoint == endpoint {
		return nil
	}
	if process := supertonicServerProcesses.processes[key]; process != nil {
		_ = process.stop(managedProcessStopTimeout)
		delete(supertonicServerProcesses.processes, key)
	}

	cmd := exec.Command(binary, "serve", "--host", host, "--port", port, "--model", "supertonic-3", "--log-level", "warning")
	cmd.Env = r.supertonicEnv(provider)
	managed, err := r.startManagedProcess(managedProcessOptions{
		cmd:      cmd,
		logName:  "supertonic-server.log",
		waitName: "localruntime.supertonicWait",
	})
	if err != nil {
		return err
	}
	process := &supertonicServerProcess{
		managedProcess: managed,
		endpoint:       endpoint,
	}
	supertonicServerProcesses.processes[key] = process
	if err := r.waitHTTPReady(ctx, httpReadyOptions{
		url:     strings.TrimRight(endpoint, "/") + "/health",
		timeout: 90 * time.Second,
		process: managed,
		ready: func(response *http.Response) bool {
			return response.StatusCode >= 200 && response.StatusCode < 500
		},
		exitedMessage:  "supertonic server exited before it was ready",
		timeoutMessage: "supertonic server did not start",
	}); err != nil {
		delete(supertonicServerProcesses.processes, key)
		_ = process.stop(managedProcessStopTimeout)
		return err
	}
	return nil
}

func (r *Runtime) stopSupertonicServerProcess(provider setup.VoiceProviderOption) error {
	key := r.localVoiceProcessKey(provider)
	supertonicServerProcesses.mu.Lock()
	process := supertonicServerProcesses.processes[key]
	delete(supertonicServerProcesses.processes, key)
	supertonicServerProcesses.mu.Unlock()
	if process == nil {
		return nil
	}
	return process.stop(managedProcessStopTimeout)
}

func (r *Runtime) supertonicServerProcessRunning(provider setup.VoiceProviderOption) bool {
	key := r.localVoiceProcessKey(provider)
	supertonicServerProcesses.mu.Lock()
	process := supertonicServerProcesses.processes[key]
	supertonicServerProcesses.mu.Unlock()
	return process != nil && process.running()
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
