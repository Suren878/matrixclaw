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
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/procsup"
	"github.com/Suren878/matrixclaw/internal/setup"
)

const (
	defaultWhisperServerEndpoint    = "http://127.0.0.1:5011"
	defaultSupertonicServerEndpoint = "http://127.0.0.1:7788"
	// anyHTTPStatus makes httpProbe accept any answer.
	anyHTTPStatus = 600
)

// server starts the provider's long-running process, or returns the one
// already running with the same command.
func (r *Runtime) server(ctx context.Context, moduleID string, provider VoiceProvider) (*procsup.Process, error) {
	var spec procsup.Spec
	var err error
	switch provider.ID {
	case "whispercpp":
		spec, err = r.whisperServerSpec(moduleID, provider)
	case "supertonic":
		spec, err = r.supertonicServerSpec(provider)
	case "piper":
		spec, err = r.piperServerSpec(moduleID, provider)
	default:
		return nil, fmt.Errorf("local voice provider %q cannot be started", provider.ID)
	}
	if err != nil {
		return nil, err
	}
	return r.procs.Start(ctx, spec)
}

func (r *Runtime) whisperServerSpec(moduleID string, provider VoiceProvider) (procsup.Spec, error) {
	if installed, _ := r.VoiceModelInstalled(moduleID, provider); !installed {
		return procsup.Spec{}, fmt.Errorf("whisper.cpp model is not installed")
	}
	binary, err := r.WhisperServerPath(provider)
	if err != nil {
		return procsup.Spec{}, err
	}
	endpoint := serverEndpoint(provider, defaultWhisperServerEndpoint)
	host, port := endpointHostPort(endpoint, "5011")
	args := []string{"--model", r.VoiceModelPath(moduleID, provider), "--host", host, "--port", port, "--convert"}
	if language := whisperLanguageArg(provider.Config.Language); language != "" {
		args = append(args, "--language", language)
	}
	if provider.Config.Threads > 0 {
		args = append(args, "--threads", strconv.Itoa(provider.Config.Threads))
	}
	return procsup.Spec{
		Key:          provider.ID,
		Path:         binary,
		Args:         args,
		Log:          "whisper-server.log",
		Ready:        r.httpProbe(endpoint, anyHTTPStatus),
		ReadyTimeout: 60 * time.Second,
	}, nil
}

func (r *Runtime) supertonicServerSpec(provider VoiceProvider) (procsup.Spec, error) {
	binary, err := r.VoiceBinaryPath(provider)
	if err != nil {
		return procsup.Spec{}, err
	}
	endpoint := serverEndpoint(provider, defaultSupertonicServerEndpoint)
	host, port := endpointHostPort(endpoint, "7788")
	return procsup.Spec{
		Key:          provider.ID,
		Path:         binary,
		Args:         []string{"serve", "--host", host, "--port", port, "--model", "supertonic-3", "--log-level", "warning"},
		Env:          r.supertonicEnv(provider),
		Log:          "supertonic-server.log",
		Ready:        r.httpProbe(endpoint+"/health", http.StatusInternalServerError),
		ReadyTimeout: 90 * time.Second,
	}, nil
}

func (r *Runtime) piperServerSpec(moduleID string, provider VoiceProvider) (procsup.Spec, error) {
	if installed, _ := r.VoiceModelInstalled(moduleID, provider); !installed {
		return procsup.Spec{}, fmt.Errorf("voice is not installed")
	}
	binary, err := r.VoiceBinaryPath(provider)
	if err != nil {
		return procsup.Spec{}, err
	}
	modelPath := r.VoiceModelPath(moduleID, provider)
	if err := os.MkdirAll(r.piperOutputDir(provider), 0o755); err != nil {
		return procsup.Spec{}, err
	}
	return procsup.Spec{
		Key:   provider.ID,
		Path:  binary,
		Args:  []string{"--model", modelPath, "--config", modelPath + ".json", "--output-dir", r.piperOutputDir(provider), "--output-dir-naming", "timestamp"},
		Stdin: true,
		Log:   "piper.log",
	}, nil
}

// httpProbe passes once url answers with a status below limit.
func (r *Runtime) httpProbe(url string, limit int) func(context.Context) error {
	return func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		response, err := r.httpClient().Do(request)
		if err != nil {
			return err
		}
		_ = response.Body.Close()
		if response.StatusCode >= limit {
			return fmt.Errorf("%s answered %s", url, response.Status)
		}
		return nil
	}
}

func serverEndpoint(provider VoiceProvider, fallback string) string {
	if endpoint := strings.TrimSpace(provider.Config.Endpoint); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	return fallback
}

func endpointHostPort(endpoint string, defaultPort string) (string, string) {
	host, port := "127.0.0.1", defaultPort
	if parsed, err := url.Parse(endpoint); err == nil {
		if parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
		if parsed.Port() != "" {
			port = parsed.Port()
		}
	}
	return host, port
}

func (r *Runtime) supertonicServerTextToSpeech(ctx context.Context, provider VoiceProvider, text string) ([]byte, error) {
	if _, err := r.server(ctx, setup.VoiceModuleTTS, provider); err != nil {
		return nil, err
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
	endpoint := serverEndpoint(provider, defaultSupertonicServerEndpoint) + "/v1/tts"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := r.httpClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
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
