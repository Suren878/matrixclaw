package localruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

const (
	ActionDownload = "download"
	ActionDelete   = "delete"
	ActionStart    = "start"
	ActionStop     = "stop"

	RuntimeUnavailable    = "unavailable"
	RuntimeNotImplemented = "not_implemented"
	RuntimeRunning        = "running"
	RuntimeStopped        = "stopped"
)

type Runtime struct {
	root   string
	client *http.Client
}

func New(root string) *Runtime {
	return &Runtime{
		root:   strings.TrimSpace(root),
		client: &http.Client{Timeout: 30 * time.Minute},
	}
}

func (r *Runtime) DecorateVoiceModules(modules []setup.VoiceModuleDescriptor) []setup.VoiceModuleDescriptor {
	out := append([]setup.VoiceModuleDescriptor(nil), modules...)
	for i := range out {
		for j := range out[i].Providers {
			out[i].Providers[j] = r.DecorateVoiceProvider(out[i].ID, out[i].Providers[j])
			if out[i].Providers[j].ID == out[i].ProviderID {
				out[i].Status = "Disabled"
				out[i].Config = out[i].Providers[j].Config
				out[i].Local = out[i].Providers[j].Local
				if out[i].Enabled {
					out[i].Status = out[i].Providers[j].Status
				}
			}
		}
	}
	return out
}

func (r *Runtime) DecorateVoiceProvider(moduleID string, provider setup.VoiceProviderOption) setup.VoiceProviderOption {
	if !provider.Local {
		return provider
	}
	if provider.ID == "piper" {
		if models, ok := piperCatalogModels(); ok && len(models) > 0 {
			provider.Models = models
			provider.CatalogStatus = "online"
			provider.CatalogDetail = fmt.Sprintf("%d voices", len(models))
		} else {
			provider.CatalogStatus = "fallback"
			provider.CatalogDetail = "using bundled fallback voices"
		}
	}
	if provider.ID == "whispercpp" {
		if models, ok := whisperCatalogModels(); ok && len(models) > 0 {
			provider.Models = models
			provider.CatalogStatus = "online"
			provider.CatalogDetail = fmt.Sprintf("%d models", len(models))
		} else {
			provider.CatalogStatus = "fallback"
			provider.CatalogDetail = "using bundled fallback models"
		}
	}
	provider = r.decorateVoiceModels(moduleID, provider)
	provider.RuntimeRSS = r.VoiceRuntimeRSSBytes(provider)
	installed, path := r.VoiceModelInstalled(moduleID, provider)
	provider.Downloaded = installed
	provider.ModelPath = path
	provider.Endpoint = path
	installedCount := installedVoiceModelCount(provider.Models)
	if installed {
		if provider.ID == "piper" {
			if _, err := r.VoiceBinaryPath(provider); err != nil {
				provider.RuntimeState = RuntimeUnavailable
				provider.RuntimeDetail = "Piper runtime is not installed"
				provider.Status = "Local · runtime missing"
			} else if voiceProviderRunsPerTask(provider) {
				provider.RuntimeState = RuntimeStopped
				provider.RuntimeDetail = ""
				provider.Status = "Local · run per task"
			} else if r.voiceRuntimeRunning(provider) {
				provider.RuntimeState = RuntimeRunning
				provider.RuntimeDetail = ""
				provider.Status = "Local · running"
			} else {
				provider.RuntimeState = RuntimeStopped
				provider.RuntimeDetail = ""
				provider.Status = "Local · stopped"
			}
		} else if provider.ID == "whispercpp" {
			if voiceProviderRunsPerTask(provider) {
				if _, err := r.WhisperCLIPath(provider); err != nil {
					provider.RuntimeState = RuntimeUnavailable
					provider.RuntimeDetail = "Whisper.cpp runtime is not installed"
					provider.Status = "Local · runtime missing"
				} else {
					provider.RuntimeState = RuntimeStopped
					provider.RuntimeDetail = ""
					provider.Status = "Local · run per task"
				}
			} else if _, err := r.WhisperServerPath(provider); err != nil {
				provider.RuntimeState = RuntimeUnavailable
				provider.RuntimeDetail = "Whisper.cpp runtime is not installed"
				provider.Status = "Local · runtime missing"
			} else if r.voiceRuntimeRunning(provider) {
				provider.RuntimeState = RuntimeRunning
				provider.RuntimeDetail = ""
				provider.Status = "Local · running"
			} else {
				provider.RuntimeState = RuntimeStopped
				provider.RuntimeDetail = ""
				provider.Status = "Local · stopped"
			}
		} else {
			provider.RuntimeState = RuntimeNotImplemented
			provider.RuntimeDetail = "Runtime process management is not implemented yet"
			provider.Status = "Local · installed · runtime manager unavailable"
		}
	} else {
		provider.RuntimeState = RuntimeUnavailable
		provider.RuntimeDetail = "Download the selected local files before local voice can run"
		if provider.ID == "piper" && installedCount > 0 {
			provider.Status = fmt.Sprintf("Local · active voice not installed · %d installed", installedCount)
		} else {
			provider.Status = "Local · not installed"
		}
	}
	return provider
}

func (r *Runtime) decorateVoiceModels(moduleID string, provider setup.VoiceProviderOption) setup.VoiceProviderOption {
	if !provider.Local || len(provider.Models) == 0 {
		return provider
	}
	for i := range provider.Models {
		installed, path := r.VoiceModelInstalledForID(moduleID, provider, provider.Models[i].ID)
		provider.Models[i].Installed = installed
		provider.Models[i].Path = path
	}
	return provider
}

func installedVoiceModelCount(models []setup.VoiceModelOption) int {
	count := 0
	for _, model := range models {
		if model.Installed {
			count++
		}
	}
	return count
}

func (r *Runtime) VoiceModelInstalled(moduleID string, provider setup.VoiceProviderOption) (bool, string) {
	path := r.VoiceModelPath(moduleID, provider)
	if path == "" {
		return false, ""
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir(), path
}

func (r *Runtime) VoiceRuntimeRSSBytes(provider setup.VoiceProviderOption) uint64 {
	return voiceRuntimeRSSBytes(provider)
}

func (r *Runtime) VoiceModelInstalledForID(moduleID string, provider setup.VoiceProviderOption, modelID string) (bool, string) {
	path := r.VoiceModelPathForID(moduleID, provider, modelID)
	if path == "" {
		return false, ""
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir(), path
}

func (r *Runtime) VoiceModelPath(moduleID string, provider setup.VoiceProviderOption) string {
	return r.VoiceModelPathForID(moduleID, provider, "")
}

func (r *Runtime) VoiceModelPathForID(moduleID string, provider setup.VoiceProviderOption, modelID string) string {
	root := r.rootDir()
	switch provider.ID {
	case "piper":
		voiceID := strings.TrimSpace(modelID)
		if voiceID == "" {
			voiceID = strings.TrimSpace(provider.Config.VoiceID)
		}
		if voiceID == "" {
			voiceID = "en_US-lessac-medium"
		}
		return filepath.Join(root, "voice", "tts", "piper", voiceID, voiceID+".onnx")
	case "whispercpp":
		modelID := strings.TrimSpace(modelID)
		if modelID == "" {
			modelID = strings.TrimSpace(provider.Config.ModelID)
		}
		if modelID == "" {
			modelID = "base"
		}
		return filepath.Join(root, "voice", "stt", "whispercpp", modelID, "ggml-"+modelID+".bin")
	default:
		return ""
	}
}

func voiceRuntimeProcessNames(provider setup.VoiceProviderOption) []string {
	names := []string{}
	if binary := strings.TrimSpace(provider.Config.BinaryPath); binary != "" {
		names = append(names, filepath.Base(binary))
	}
	switch provider.ID {
	case "piper":
		names = append(names, "piper", "piper-tts")
	case "whispercpp":
		names = append(names, "whisper-server", "whisper-cli", "main")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func (r *Runtime) ApplyVoiceAction(ctx context.Context, moduleID string, provider setup.VoiceProviderOption, request setup.VoiceProviderActionRequest) (setup.VoiceProviderOption, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if !provider.Local {
		return provider, errors.New("voice provider is not local")
	}
	provider = voiceProviderForActionTarget(provider, request.ModelID)
	switch action {
	case ActionDownload:
		return r.downloadVoiceModel(ctx, moduleID, provider)
	case ActionDelete:
		return r.deleteVoiceModel(moduleID, provider)
	case ActionStart:
		return r.startVoiceRuntime(ctx, moduleID, provider)
	case ActionStop:
		return r.stopVoiceRuntime(moduleID, provider)
	default:
		return provider, fmt.Errorf("unsupported local voice action %q", action)
	}
}

func voiceProviderForActionTarget(provider setup.VoiceProviderOption, modelID string) setup.VoiceProviderOption {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return provider
	}
	switch provider.ID {
	case "piper":
		provider.Config.VoiceID = modelID
	case "whispercpp":
		provider.Config.ModelID = modelID
	}
	return provider
}

func (r *Runtime) downloadVoiceModel(ctx context.Context, moduleID string, provider setup.VoiceProviderOption) (setup.VoiceProviderOption, error) {
	target := r.VoiceModelPath(moduleID, provider)
	if target == "" {
		return provider, errors.New("local voice model path is empty")
	}
	downloads, err := voiceDownloads(moduleID, provider, target)
	if err != nil {
		return provider, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return provider, err
	}
	for _, item := range downloads {
		if err := r.downloadFile(ctx, item.URL, item.Path); err != nil {
			return provider, err
		}
	}
	return r.DecorateVoiceProvider(moduleID, provider), nil
}

func (r *Runtime) deleteVoiceModel(moduleID string, provider setup.VoiceProviderOption) (setup.VoiceProviderOption, error) {
	target := r.VoiceModelPath(moduleID, provider)
	if target == "" {
		return provider, errors.New("local voice model path is empty")
	}
	paths := []string{target}
	if provider.ID == "piper" {
		paths = append(paths, target+".json")
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return provider, err
		}
	}
	_ = os.Remove(filepath.Dir(target))
	return r.DecorateVoiceProvider(moduleID, provider), nil
}

func (r *Runtime) startVoiceRuntime(ctx context.Context, moduleID string, provider setup.VoiceProviderOption) (setup.VoiceProviderOption, error) {
	if !voiceProviderPersistentRuntimeAvailable(provider) {
		return provider, fmt.Errorf("%s persistent runtime is not implemented yet", provider.Name)
	}
	if voiceProviderRunsPerTask(provider) {
		return provider, fmt.Errorf("%s is configured to run per task", provider.Name)
	}
	installed, _ := r.VoiceModelInstalled(moduleID, provider)
	if !installed {
		return provider, errors.New("local voice model is not installed")
	}
	switch provider.ID {
	case "piper":
		if _, err := r.VoiceBinaryPath(provider); err != nil {
			return provider, err
		}
		if err := r.startPiperProcess(moduleID, provider); err != nil {
			return provider, err
		}
	case "whispercpp":
		if _, err := r.WhisperServerPath(provider); err != nil {
			return provider, err
		}
		if err := r.startWhisperServerProcess(ctx, moduleID, provider); err != nil {
			return provider, err
		}
	default:
		return provider, fmt.Errorf("local voice provider %q cannot be started yet", provider.ID)
	}
	return r.DecorateVoiceProvider(moduleID, provider), nil
}

func (r *Runtime) stopVoiceRuntime(moduleID string, provider setup.VoiceProviderOption) (setup.VoiceProviderOption, error) {
	if !voiceProviderPersistentRuntimeAvailable(provider) {
		return provider, fmt.Errorf("%s persistent runtime is not implemented yet", provider.Name)
	}
	if voiceProviderRunsPerTask(provider) {
		return provider, fmt.Errorf("%s is configured to run per task", provider.Name)
	}
	switch provider.ID {
	case "piper":
		if err := r.stopPiperProcess(provider); err != nil {
			return provider, err
		}
	case "whispercpp":
		if err := r.stopWhisperServerProcess(provider); err != nil {
			return provider, err
		}
	default:
		return provider, fmt.Errorf("local voice provider %q cannot be stopped yet", provider.ID)
	}
	return r.DecorateVoiceProvider(moduleID, provider), nil
}

func (r *Runtime) voiceRuntimeRunning(provider setup.VoiceProviderOption) bool {
	switch provider.ID {
	case "piper":
		return r.piperProcessRunning(provider)
	case "whispercpp":
		return r.whisperServerProcessRunning(provider)
	}
	return false
}

func (r *Runtime) VoiceBinaryPath(provider setup.VoiceProviderOption) (string, error) {
	value := strings.TrimSpace(provider.Config.BinaryPath)
	if value == "" {
		value = provider.ID
	}
	if filepath.IsAbs(value) || strings.Contains(value, string(os.PathSeparator)) {
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value, nil
		}
	}
	if path, err := exec.LookPath(value); err == nil {
		return path, nil
	}
	if provider.ID == "piper" {
		path := filepath.Join(r.runtimeDir(), "piper-venv", "bin", "piper")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s runtime is not installed", provider.Name)
}

func (r *Runtime) PiperTextToSpeech(ctx context.Context, provider setup.VoiceProviderOption, text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("text is required")
	}
	provider = r.DecorateVoiceProvider(setup.VoiceModuleTTS, provider)
	if !voiceProviderRunsPerTask(provider) {
		return r.piperPersistentTextToSpeech(ctx, provider, text)
	}
	if !voiceProviderRuntimeRunnable(provider) {
		return nil, errors.New("Piper is stopped")
	}
	return r.piperOneShotTextToSpeech(ctx, provider, text)
}

func voiceProviderRuntimeRunnable(provider setup.VoiceProviderOption) bool {
	return voiceProviderRunsPerTask(provider) || strings.EqualFold(strings.TrimSpace(provider.RuntimeState), RuntimeRunning)
}

func voiceProviderRunsPerTask(provider setup.VoiceProviderOption) bool {
	switch strings.ToLower(strings.TrimSpace(provider.Config.RuntimeMode)) {
	case "always", "always_running", "persistent", "server":
		return !voiceProviderPersistentRuntimeAvailable(provider)
	default:
		return true
	}
}

func voiceProviderPersistentRuntimeAvailable(provider setup.VoiceProviderOption) bool {
	return provider.ID == "piper" || provider.ID == "whispercpp"
}

func (r *Runtime) downloadFile(ctx context.Context, url string, path string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := r.httpClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download %s: status %s", url, response.Status)
	}
	temp := path + ".download"
	file, err := os.Create(temp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(temp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temp)
		return closeErr
	}
	return os.Rename(temp, path)
}

func (r *Runtime) httpClient() *http.Client {
	if r != nil && r.client != nil {
		return r.client
	}
	return http.DefaultClient
}

func (r *Runtime) rootDir() string {
	if r != nil && strings.TrimSpace(r.root) != "" {
		return r.root
	}
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_LOCAL_DIR")); value != "" {
		return value
	}
	base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			base = filepath.Join(home, ".local", "state")
		}
	}
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "matrixclaw", "local")
}

func (r *Runtime) runtimeDir() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_RUNTIME_DIR")); value != "" {
		return value
	}
	root := r.rootDir()
	base := filepath.Dir(root)
	if strings.TrimSpace(base) == "" || base == "." || base == string(os.PathSeparator) {
		base = filepath.Join(os.TempDir(), "matrixclaw")
	}
	return filepath.Join(base, "runtime")
}

type downloadItem struct {
	URL  string
	Path string
}

func voiceDownloads(_ string, provider setup.VoiceProviderOption, modelPath string) ([]downloadItem, error) {
	switch provider.ID {
	case "piper":
		url, err := piperVoiceURL(provider.Config.VoiceID)
		if err != nil {
			return nil, err
		}
		return []downloadItem{
			{URL: url + ".onnx", Path: modelPath},
			{URL: url + ".onnx.json", Path: modelPath + ".json"},
		}, nil
	case "whispercpp":
		url, err := whisperModelURL(provider.Config.ModelID)
		if err != nil {
			return nil, err
		}
		return []downloadItem{{URL: url, Path: modelPath}}, nil
	default:
		return nil, fmt.Errorf("local voice provider %q cannot download models", provider.ID)
	}
}

func piperVoiceURL(voiceID string) (string, error) {
	voiceID = strings.TrimSpace(voiceID)
	if voiceID == "" {
		voiceID = "en_US-lessac-medium"
	}
	language, voice, quality, ok := splitPiperVoiceID(voiceID)
	if !ok {
		return "", fmt.Errorf("piper voice %q does not have a recognized catalog id", voiceID)
	}
	family, _, _ := strings.Cut(language, "_")
	if family == "" {
		return "", fmt.Errorf("piper voice %q does not have a recognized language", voiceID)
	}
	return "https://huggingface.co/rhasspy/piper-voices/resolve/main/" + family + "/" + language + "/" + voice + "/" + quality + "/" + voiceID, nil
}

func splitPiperVoiceID(voiceID string) (language string, voice string, quality string, ok bool) {
	voiceID = strings.TrimSpace(voiceID)
	firstDash := strings.Index(voiceID, "-")
	lastDash := strings.LastIndex(voiceID, "-")
	if firstDash <= 0 || lastDash <= firstDash {
		return "", "", "", false
	}
	language = voiceID[:firstDash]
	voice = voiceID[firstDash+1 : lastDash]
	quality = voiceID[lastDash+1:]
	if language == "" || voice == "" || quality == "" {
		return "", "", "", false
	}
	return language, voice, quality, true
}

func whisperModelURL(modelID string) (string, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = "base"
	}
	if strings.ContainsAny(modelID, `/\`) || strings.Contains(modelID, "..") {
		return "", fmt.Errorf("whisper.cpp model %q does not have a download URL", modelID)
	}
	return "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-" + modelID + ".bin", nil
}
