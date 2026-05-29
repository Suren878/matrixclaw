package localruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

type supertonicDriver struct{}

func (supertonicDriver) actionIDs() setup.VoiceProviderActionIDs {
	return voiceProviderActionIDsRuntimeOnly()
}

func (supertonicDriver) decorate(r *Runtime, _ string, provider setup.VoiceProviderOption) setup.VoiceProviderOption {
	if models, ok := supertonicCatalogModels(); ok && len(models) > 0 {
		provider.Models = models
		provider.CatalogStatus = "online"
		provider.CatalogDetail = fmt.Sprintf("%d voice styles · 31 languages", len(models))
	} else {
		provider.Models = models
		provider.CatalogStatus = "fallback"
		provider.CatalogDetail = "using default voice style"
	}
	provider = ensureConfiguredSupertonicVoiceModel(provider)
	provider.RuntimeRSS = r.VoiceRuntimeRSSBytes(provider)
	if path, err := r.ManagedVoiceBinaryPath(provider); err == nil {
		provider.RuntimeInstalled = true
		provider.RuntimePath = path
	}
	if !r.supertonicRuntimeComplete() {
		provider.RuntimeInstalled = false
		provider.RuntimePath = ""
	}
	provider.Downloaded = provider.RuntimeInstalled
	provider.ModelPath = ""
	if !provider.RuntimeInstalled {
		provider.RuntimeState = RuntimeUnavailable
		provider.RuntimeDetail = provider.Name + " runtime is not installed"
		provider.Status = "Local · runtime missing"
	} else if voiceProviderRunsPerTask(provider) {
		provider.RuntimeState = RuntimeStopped
		provider.RuntimeDetail = ""
		provider.Status = "Local · run per task"
	} else if r.supertonicServerProcessRunning(provider) {
		provider.RuntimeState = RuntimeRunning
		provider.RuntimeDetail = ""
		provider.Status = "Local · running"
	} else {
		provider.RuntimeState = RuntimeStopped
		provider.RuntimeDetail = ""
		provider.Status = "Local · stopped"
	}
	return provider
}

func (supertonicDriver) modelPath(*Runtime, string, setup.VoiceProviderOption, string) string {
	return ""
}

func (supertonicDriver) modelInstalled(string) bool {
	return false
}

func (supertonicDriver) downloads(provider setup.VoiceProviderOption, modelPath string) ([]downloadItem, error) {
	url, err := supertonicVoiceStyleURL(provider.Config.VoiceID)
	if err != nil {
		return nil, err
	}
	return []downloadItem{{URL: url, Path: modelPath}}, nil
}

func (supertonicDriver) actionTarget(provider setup.VoiceProviderOption, modelID string) setup.VoiceProviderOption {
	if modelID = strings.TrimSpace(modelID); modelID != "" {
		provider.Config.VoiceID = strings.ToUpper(modelID)
	}
	return provider
}

func (supertonicDriver) installRuntime(ctx context.Context, r *Runtime, _ string, _ setup.VoiceProviderOption) error {
	return r.installSupertonicRuntime(ctx)
}

func (supertonicDriver) deleteRuntime(r *Runtime, _ string, provider setup.VoiceProviderOption) error {
	if err := r.stopSupertonicServerProcess(provider); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Dir(filepath.Dir(r.managedSupertonicBinaryPath()))); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(r.runtimeDir(), "supertonic3"))
}

func (supertonicDriver) startRuntime(ctx context.Context, r *Runtime, _ string, provider setup.VoiceProviderOption) error {
	if _, err := r.VoiceBinaryPath(provider); err != nil {
		return err
	}
	return r.startSupertonicServerProcess(ctx, provider)
}

func (supertonicDriver) stopRuntime(r *Runtime, _ string, provider setup.VoiceProviderOption) error {
	return r.stopSupertonicServerProcess(provider)
}

func (supertonicDriver) stopForDelete(r *Runtime, provider setup.VoiceProviderOption) error {
	return r.stopSupertonicServerProcess(provider)
}

func (supertonicDriver) runtimeRunning(r *Runtime, provider setup.VoiceProviderOption) bool {
	return r.supertonicServerProcessRunning(provider)
}

func (supertonicDriver) managedBinaryPath(r *Runtime, provider setup.VoiceProviderOption) (string, error) {
	return localManagedBinaryPath(provider, r.managedSupertonicBinaryPath)
}

func (supertonicDriver) voiceBinaryPath(r *Runtime, provider setup.VoiceProviderOption) (string, error) {
	return localVoiceBinaryPath(r, provider, r.supertonicModelCacheComplete, r.managedSupertonicBinaryPath)
}

func (supertonicDriver) processNames(provider setup.VoiceProviderOption) []string {
	return append(configuredBinaryProcessName(provider), "supertonic")
}

func (r *Runtime) managedSupertonicBinaryPath() string {
	return filepath.Join(r.runtimeDir(), "supertonic-venv", "bin", "supertonic")
}

func (r *Runtime) managedSupertonicPythonPath() string {
	return filepath.Join(r.runtimeDir(), "supertonic-venv", "bin", "python")
}

func (r *Runtime) supertonicInstallMarkerPath() string {
	return filepath.Join(r.runtimeDir(), "supertonic-venv", ".matrixclaw-installed")
}

func (r *Runtime) supertonicRuntimeComplete() bool {
	return r.supertonicBinaryInstalled() && r.supertonicModelCacheComplete()
}

func (r *Runtime) supertonicBinaryInstalled() bool {
	if info, err := os.Stat(r.managedSupertonicBinaryPath()); err != nil || info.IsDir() {
		return false
	}
	return true
}

func (r *Runtime) supertonicModelCacheComplete() bool {
	for _, dir := range r.supertonicModelCacheDirs() {
		if supertonicModelCacheDirComplete(dir) {
			return true
		}
	}
	return false
}

func supertonicModelCacheDirComplete(dir string) bool {
	for _, path := range []string{
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "voice_styles", "M1.json"),
		filepath.Join(dir, "onnx", "tts.json"),
		filepath.Join(dir, "onnx", "text_encoder.onnx"),
		filepath.Join(dir, "onnx", "duration_predictor.onnx"),
		filepath.Join(dir, "onnx", "vector_estimator.onnx"),
		filepath.Join(dir, "onnx", "vocoder.onnx"),
	} {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			return false
		}
	}
	return true
}

func (r *Runtime) supertonicModelCacheDirs() []string {
	dirs := []string{r.supertonicModelCacheDir()}
	if value := strings.TrimSpace(os.Getenv("SUPERTONIC_CACHE_DIR")); value == "" {
		if cacheDir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(cacheDir) != "" {
			dirs = append(dirs, filepath.Join(cacheDir, "supertonic3"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".cache", "supertonic3"))
		}
	}
	return uniqueLocalStrings(dirs...)
}

func (r *Runtime) supertonicModelCacheDir() string {
	if value := strings.TrimSpace(os.Getenv("SUPERTONIC_CACHE_DIR")); value != "" {
		if strings.HasPrefix(value, "~"+string(os.PathSeparator)) {
			if home, err := os.UserHomeDir(); err == nil {
				return filepath.Join(home, strings.TrimPrefix(value, "~"+string(os.PathSeparator)))
			}
		}
		return value
	}
	return filepath.Join(r.runtimeDir(), "supertonic3")
}

func (r *Runtime) supertonicActiveCacheDir() string {
	for _, dir := range r.supertonicModelCacheDirs() {
		if supertonicModelCacheDirComplete(dir) {
			return dir
		}
	}
	return r.supertonicModelCacheDir()
}

func (r *Runtime) supertonicEnv(provider setup.VoiceProviderOption) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env, "SUPERTONIC_CACHE_DIR="+r.supertonicActiveCacheDir())
	if provider.Config.Threads > 0 {
		threadCount := strconv.Itoa(provider.Config.Threads)
		env = append(env,
			"SUPERTONIC_INTRA_OP_THREADS="+threadCount,
			"SUPERTONIC_INTER_OP_THREADS="+threadCount,
		)
	}
	return env
}

func (r *Runtime) installSupertonicRuntime(ctx context.Context) error {
	if err := requireFFmpegForLocalTTS(); err != nil {
		return err
	}
	if r.supertonicRuntimeComplete() {
		return nil
	}
	if !r.supertonicBinaryInstalled() {
		python, err := exec.LookPath("python3")
		if err != nil {
			python, err = exec.LookPath("python")
		}
		if err != nil {
			return fmt.Errorf("python 3 is required to install Supertonic runtime")
		}
		venvDir := filepath.Dir(filepath.Dir(r.managedSupertonicBinaryPath()))
		if err := os.RemoveAll(venvDir); err != nil {
			return err
		}
		if err := os.MkdirAll(r.runtimeDir(), 0o755); err != nil {
			return err
		}
		if err := runRuntimeCommand(ctx, python, "-m", "venv", venvDir); err != nil {
			return err
		}
		venvPython := r.managedSupertonicPythonPath()
		if err := runRuntimeCommand(ctx, venvPython, "-m", "pip", "install", "--upgrade", "pip"); err != nil {
			return err
		}
		if err := runRuntimeCommand(ctx, venvPython, "-m", "pip", "install", "supertonic[serve]"); err != nil {
			return err
		}
		if !r.supertonicBinaryInstalled() {
			return fmt.Errorf("supertonic runtime installation finished without supertonic binary")
		}
	}
	if !r.supertonicModelCacheComplete() {
		if err := runRuntimeCommandWithEnv(ctx, r.supertonicEnv(setup.VoiceProviderOption{}), r.managedSupertonicBinaryPath(), "download"); err != nil {
			return err
		}
	}
	if !r.supertonicModelCacheComplete() {
		return fmt.Errorf("supertonic runtime installation finished without downloaded model files")
	}
	if err := os.MkdirAll(filepath.Dir(r.supertonicInstallMarkerPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(r.supertonicInstallMarkerPath(), []byte("ok\n"), 0o644); err != nil {
		return err
	}
	return nil
}

func ensureConfiguredSupertonicVoiceModel(provider setup.VoiceProviderOption) setup.VoiceProviderOption {
	voiceID := strings.ToUpper(strings.TrimSpace(provider.Config.VoiceID))
	if voiceID == "" {
		return provider
	}
	for _, model := range provider.Models {
		if strings.EqualFold(model.ID, voiceID) {
			return provider
		}
	}
	provider.Models = append(provider.Models, supertonicVoiceModel(voiceID, 0))
	return provider
}
