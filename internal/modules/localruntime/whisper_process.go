package localruntime

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

var whisperServerProcesses = &localProcessManager[*whisperServerProcess]{
	processes: map[string]*whisperServerProcess{},
}

type whisperServerProcess struct {
	*managedProcess
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
	key := r.localVoiceProcessKey(provider)

	whisperServerProcesses.mu.Lock()
	defer whisperServerProcesses.mu.Unlock()
	if process := whisperServerProcesses.processes[key]; process != nil && process.running() && process.modelPath == modelPath && process.endpoint == endpoint {
		return nil
	}
	if process := whisperServerProcesses.processes[key]; process != nil {
		_ = process.stop(managedProcessStopTimeout)
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
	managed, err := r.startManagedProcess(managedProcessOptions{
		cmd:      cmd,
		logName:  "whisper-server.log",
		waitName: "localruntime.whisperWait",
	})
	if err != nil {
		return err
	}
	process := &whisperServerProcess{
		managedProcess: managed,
		modelPath:      modelPath,
		endpoint:       endpoint,
	}
	whisperServerProcesses.processes[key] = process
	if err := r.waitHTTPReady(ctx, httpReadyOptions{
		url:            strings.TrimRight(endpoint, "/"),
		timeout:        60 * time.Second,
		process:        managed,
		exitedMessage:  "whisper.cpp server exited before it was ready",
		timeoutMessage: "whisper.cpp server did not start",
	}); err != nil {
		delete(whisperServerProcesses.processes, key)
		_ = process.stop(managedProcessStopTimeout)
		return err
	}
	return nil
}

func (r *Runtime) stopWhisperServerProcess(provider setup.VoiceProviderOption) error {
	key := r.localVoiceProcessKey(provider)
	whisperServerProcesses.mu.Lock()
	process := whisperServerProcesses.processes[key]
	delete(whisperServerProcesses.processes, key)
	whisperServerProcesses.mu.Unlock()
	if process == nil {
		return nil
	}
	return process.stop(managedProcessStopTimeout)
}

func (r *Runtime) whisperServerProcessRunning(provider setup.VoiceProviderOption) bool {
	key := r.localVoiceProcessKey(provider)
	whisperServerProcesses.mu.Lock()
	process := whisperServerProcesses.processes[key]
	whisperServerProcesses.mu.Unlock()
	return process != nil && process.running()
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
