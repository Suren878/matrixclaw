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
	ActionDownload       = "download"
	ActionDelete         = "delete"
	ActionInstallRuntime = "install-runtime"
	ActionDeleteRuntime  = "delete-runtime"
	ActionStart          = "start"
	ActionStop           = "stop"

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
	if driver, ok := driverForProvider(provider.ID); ok {
		provider.ActionIDs = driver.actionIDs()
		return driver.decorate(r, moduleID, provider)
	}
	return decorateGenericLocalVoiceProvider(r, moduleID, provider)
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
	return voiceModelPathInstalled(provider, path), path
}

func (r *Runtime) VoiceRuntimeRSSBytes(provider setup.VoiceProviderOption) uint64 {
	return voiceRuntimeRSSBytes(provider)
}

func (r *Runtime) VoiceModelInstalledForID(moduleID string, provider setup.VoiceProviderOption, modelID string) (bool, string) {
	path := r.VoiceModelPathForID(moduleID, provider, modelID)
	if path == "" {
		return false, ""
	}
	return voiceModelPathInstalled(provider, path), path
}

func voiceModelPathInstalled(provider setup.VoiceProviderOption, path string) bool {
	if driver, ok := driverForProvider(provider.ID); ok {
		return driver.modelInstalled(path)
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func (r *Runtime) VoiceModelPath(moduleID string, provider setup.VoiceProviderOption) string {
	return r.VoiceModelPathForID(moduleID, provider, "")
}

func (r *Runtime) VoiceModelPathForID(moduleID string, provider setup.VoiceProviderOption, modelID string) string {
	if driver, ok := driverForProvider(provider.ID); ok {
		return driver.modelPath(r, moduleID, provider, modelID)
	}
	return ""
}

func voiceRuntimeProcessNames(provider setup.VoiceProviderOption) []string {
	names := []string{}
	if driver, ok := driverForProvider(provider.ID); ok {
		names = append(names, driver.processNames(provider)...)
	} else if binary := strings.TrimSpace(provider.Config.BinaryPath); binary != "" {
		names = append(names, filepath.Base(binary))
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
	provider = r.DecorateVoiceProvider(moduleID, provider)
	switch action {
	case ActionDownload:
		if provider.ID == "supertonic" {
			return provider, fmt.Errorf("%s uses a shared runtime download; install the runtime instead", provider.Name)
		}
		return r.downloadVoiceModel(ctx, moduleID, provider)
	case ActionDelete:
		return r.deleteVoiceModel(moduleID, provider)
	case ActionInstallRuntime:
		return r.installVoiceRuntime(ctx, moduleID, provider)
	case ActionDeleteRuntime:
		return r.deleteVoiceRuntime(moduleID, provider)
	case ActionStart:
		return r.startVoiceRuntime(ctx, moduleID, provider)
	case ActionStop:
		return r.stopVoiceRuntime(moduleID, provider)
	default:
		return provider, fmt.Errorf("unsupported local voice action %q", action)
	}
}

func (r *Runtime) installVoiceRuntime(ctx context.Context, moduleID string, provider setup.VoiceProviderOption) (setup.VoiceProviderOption, error) {
	driver, ok := driverForProvider(provider.ID)
	if !ok {
		return provider, fmt.Errorf("%s runtime installation is not implemented yet", provider.Name)
	}
	if err := driver.installRuntime(ctx, r, moduleID, provider); err != nil {
		return provider, err
	}
	return r.DecorateVoiceProvider(moduleID, provider), nil
}

func (r *Runtime) deleteVoiceRuntime(moduleID string, provider setup.VoiceProviderOption) (setup.VoiceProviderOption, error) {
	driver, ok := driverForProvider(provider.ID)
	if !ok {
		return provider, fmt.Errorf("%s runtime deletion is not implemented yet", provider.Name)
	}
	if err := driver.deleteRuntime(r, moduleID, provider); err != nil {
		return provider, err
	}
	return r.DecorateVoiceProvider(moduleID, provider), nil
}

func voiceProviderForActionTarget(provider setup.VoiceProviderOption, modelID string) setup.VoiceProviderOption {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return provider
	}
	if driver, ok := driverForProvider(provider.ID); ok {
		return driver.actionTarget(provider, modelID)
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
	if err := r.stopVoiceRuntimeForDelete(provider); err != nil {
		return provider, err
	}
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

func (r *Runtime) stopVoiceRuntimeForDelete(provider setup.VoiceProviderOption) error {
	if driver, ok := driverForProvider(provider.ID); ok {
		return driver.stopForDelete(r, provider)
	}
	return nil
}

func (r *Runtime) startVoiceRuntime(ctx context.Context, moduleID string, provider setup.VoiceProviderOption) (setup.VoiceProviderOption, error) {
	if !voiceProviderPersistentRuntimeAvailable(provider) {
		return provider, fmt.Errorf("%s persistent runtime is not implemented yet", provider.Name)
	}
	if voiceProviderRunsPerTask(provider) {
		return provider, fmt.Errorf("%s is configured to run per task", provider.Name)
	}
	if provider.ID == "supertonic" {
		if _, err := r.VoiceBinaryPath(provider); err != nil {
			return provider, err
		}
	} else {
		installed, _ := r.VoiceModelInstalled(moduleID, provider)
		if !installed {
			return provider, errors.New("local voice model is not installed")
		}
	}
	driver, ok := driverForProvider(provider.ID)
	if !ok {
		return provider, fmt.Errorf("local voice provider %q cannot be started yet", provider.ID)
	}
	if err := driver.startRuntime(ctx, r, moduleID, provider); err != nil {
		return provider, err
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
	driver, ok := driverForProvider(provider.ID)
	if !ok {
		return provider, fmt.Errorf("local voice provider %q cannot be stopped yet", provider.ID)
	}
	if err := driver.stopRuntime(r, moduleID, provider); err != nil {
		return provider, err
	}
	return r.DecorateVoiceProvider(moduleID, provider), nil
}

func (r *Runtime) voiceRuntimeRunning(provider setup.VoiceProviderOption) bool {
	if driver, ok := driverForProvider(provider.ID); ok {
		return driver.runtimeRunning(r, provider)
	}
	return false
}

func (r *Runtime) VoiceBinaryPath(provider setup.VoiceProviderOption) (string, error) {
	if driver, ok := driverForProvider(provider.ID); ok {
		return driver.voiceBinaryPath(r, provider)
	}
	return "", fmt.Errorf("%s runtime is not installed", provider.Name)
}

func (r *Runtime) ManagedVoiceBinaryPath(provider setup.VoiceProviderOption) (string, error) {
	if driver, ok := driverForProvider(provider.ID); ok {
		return driver.managedBinaryPath(r, provider)
	}
	return "", fmt.Errorf("%s runtime is not installed", provider.Name)
}

func uniqueLocalStrings(values ...string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func requireFFmpegForLocalTTS() error {
	if hasFFmpeg() {
		return nil
	}
	return errors.New("ffmpeg is required for local TTS MP3 output; install it or run scripts/install_voice_runtime.sh --all")
}

func requireFFmpegForLocalSTT() error {
	if hasFFmpeg() {
		return nil
	}
	return errors.New("ffmpeg is required for local STT audio conversion; install it or run scripts/install_voice_runtime.sh --all")
}

func hasFFmpeg() bool {
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		return true
	}
	return false
}

func runRuntimeCommand(ctx context.Context, name string, args ...string) error {
	return runRuntimeCommandWithEnv(ctx, nil, name, args...)
}

func runRuntimeCommandWithEnv(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s %s failed: %s", filepath.Base(name), strings.Join(args, " "), message)
	}
	return nil
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

func (r *Runtime) SupertonicTextToSpeech(ctx context.Context, provider setup.VoiceProviderOption, text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("text is required")
	}
	provider = r.DecorateVoiceProvider(setup.VoiceModuleTTS, provider)
	if !voiceProviderRunsPerTask(provider) {
		return r.supertonicServerTextToSpeech(ctx, provider, text)
	}
	if !voiceProviderRuntimeRunnable(provider) {
		return nil, errors.New("Supertonic is stopped")
	}
	return r.supertonicOneShotTextToSpeech(ctx, provider, text)
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
	return provider.ID == "piper" || provider.ID == "whispercpp" || provider.ID == "supertonic"
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
	if driver, ok := driverForProvider(provider.ID); ok {
		return driver.downloads(provider, modelPath)
	}
	return nil, fmt.Errorf("local voice provider %q cannot download models", provider.ID)
}
