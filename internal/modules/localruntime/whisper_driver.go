package localruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

type whisperDriver struct{}

func (whisperDriver) actionIDs() setup.VoiceProviderActionIDs {
	return voiceProviderActionIDsWithModels()
}

func (whisperDriver) decorate(r *Runtime, moduleID string, provider setup.VoiceProviderOption) setup.VoiceProviderOption {
	if models, ok := whisperCatalogModels(); ok && len(models) > 0 {
		provider.Models = models
		provider.CatalogStatus = "online"
		provider.CatalogDetail = fmt.Sprintf("%d models", len(models))
	} else {
		provider.CatalogStatus = "fallback"
		provider.CatalogDetail = "using bundled fallback models"
	}
	provider, installed, _ := r.decorateProviderModelFiles(moduleID, provider)
	if !installed {
		provider.RuntimeState = RuntimeUnavailable
		provider.RuntimeDetail = "Download the selected local files before local voice can run"
		provider.Status = "Local · not installed"
		return provider
	}
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
	return provider
}

func (whisperDriver) modelPath(r *Runtime, _ string, provider setup.VoiceProviderOption, modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = strings.TrimSpace(provider.Config.ModelID)
	}
	if modelID == "" {
		modelID = "base"
	}
	return filepath.Join(r.rootDir(), "voice", "stt", "whispercpp", modelID, "ggml-"+modelID+".bin")
}

func (whisperDriver) modelInstalled(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func (whisperDriver) downloads(provider setup.VoiceProviderOption, modelPath string) ([]downloadItem, error) {
	url, err := whisperModelURL(provider.Config.ModelID)
	if err != nil {
		return nil, err
	}
	return []downloadItem{{URL: url, Path: modelPath}}, nil
}

func (whisperDriver) actionTarget(provider setup.VoiceProviderOption, modelID string) setup.VoiceProviderOption {
	if modelID = strings.TrimSpace(modelID); modelID != "" {
		provider.Config.ModelID = modelID
	}
	return provider
}

func (whisperDriver) installRuntime(ctx context.Context, r *Runtime, _ string, _ setup.VoiceProviderOption) error {
	return r.installWhisperRuntime(ctx)
}

func (whisperDriver) deleteRuntime(r *Runtime, _ string, provider setup.VoiceProviderOption) error {
	if err := r.stopWhisperServerProcess(provider); err != nil {
		return err
	}
	if err := os.RemoveAll(r.managedWhisperRuntimeDir()); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(r.rootDir(), "voice", "stt", "whispercpp"))
}

func (whisperDriver) startRuntime(ctx context.Context, r *Runtime, moduleID string, provider setup.VoiceProviderOption) error {
	if _, err := r.WhisperServerPath(provider); err != nil {
		return err
	}
	return r.startWhisperServerProcess(ctx, moduleID, provider)
}

func (whisperDriver) stopRuntime(r *Runtime, _ string, provider setup.VoiceProviderOption) error {
	return r.stopWhisperServerProcess(provider)
}

func (whisperDriver) stopForDelete(r *Runtime, provider setup.VoiceProviderOption) error {
	return r.stopWhisperServerProcess(provider)
}

func (whisperDriver) runtimeRunning(r *Runtime, provider setup.VoiceProviderOption) bool {
	return r.whisperServerProcessRunning(provider)
}

func (whisperDriver) managedBinaryPath(r *Runtime, provider setup.VoiceProviderOption) (string, error) {
	value := strings.TrimSpace(provider.Config.BinaryPath)
	if filepath.IsAbs(value) || strings.Contains(value, string(os.PathSeparator)) {
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value, nil
		}
	}
	path := r.managedWhisperCLIPath()
	if executableFileExists(path) && executableFileExists(r.managedWhisperServerPath()) {
		return path, nil
	}
	return "", fmt.Errorf("%s runtime is not installed", provider.Name)
}

func (whisperDriver) voiceBinaryPath(r *Runtime, provider setup.VoiceProviderOption) (string, error) {
	if path, err := r.ManagedVoiceBinaryPath(provider); err == nil {
		return path, nil
	}
	value := strings.TrimSpace(provider.Config.BinaryPath)
	if value == "" {
		value = provider.ID
	}
	if path, err := exec.LookPath(value); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%s runtime is not installed", provider.Name)
}

func (whisperDriver) processNames(provider setup.VoiceProviderOption) []string {
	return append(configuredBinaryProcessName(provider), "whisper-server", "whisper-cli", "main")
}

func (r *Runtime) managedWhisperRuntimeDir() string {
	return filepath.Join(r.runtimeDir(), "whisper.cpp")
}

func (r *Runtime) managedWhisperCLIPath() string {
	return filepath.Join(r.managedWhisperRuntimeDir(), "build", "bin", "whisper-cli")
}

func (r *Runtime) managedWhisperServerPath() string {
	return filepath.Join(r.managedWhisperRuntimeDir(), "build", "bin", "whisper-server")
}

func (r *Runtime) installWhisperRuntime(ctx context.Context) error {
	if err := requireFFmpegForLocalSTT(); err != nil {
		return err
	}
	cliPath := r.managedWhisperCLIPath()
	serverPath := r.managedWhisperServerPath()
	if executableFileExists(cliPath) && executableFileExists(serverPath) {
		return nil
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git is required to install Whisper.cpp runtime")
	}
	cmake, err := exec.LookPath("cmake")
	if err != nil {
		return fmt.Errorf("cmake is required to install Whisper.cpp runtime")
	}
	if _, err := cxxCompilerPath(); err != nil {
		return err
	}
	sourceDir := r.managedWhisperRuntimeDir()
	if err := os.MkdirAll(r.runtimeDir(), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(sourceDir, ".git")); err == nil {
		if err := runRuntimeCommand(ctx, git, "-C", sourceDir, "fetch", "--depth", "1", "origin"); err != nil {
			return err
		}
		if err := runRuntimeCommand(ctx, git, "-C", sourceDir, "reset", "--hard", "FETCH_HEAD"); err != nil {
			return err
		}
	} else if _, err := os.Stat(sourceDir); err == nil {
		return fmt.Errorf("%s exists but is not a git checkout", sourceDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		repo := strings.TrimSpace(os.Getenv("MATRIXCLAW_WHISPER_CPP_REPO"))
		if repo == "" {
			repo = "https://github.com/ggml-org/whisper.cpp.git"
		}
		if err := runRuntimeCommand(ctx, git, "clone", "--depth", "1", repo, sourceDir); err != nil {
			return err
		}
	}
	buildDir := filepath.Join(sourceDir, "build")
	if err := runRuntimeCommand(ctx, cmake, "-S", sourceDir, "-B", buildDir, "-DWHISPER_BUILD_TESTS=OFF", "-DWHISPER_BUILD_EXAMPLES=ON", "-DCMAKE_BUILD_TYPE=Release"); err != nil {
		return err
	}
	if err := runRuntimeCommand(ctx, cmake, "--build", buildDir, "-j", "4", "--config", "Release", "--target", "whisper-cli", "whisper-server"); err != nil {
		return err
	}
	if !executableFileExists(cliPath) {
		return fmt.Errorf("whisper.cpp runtime installation finished without whisper-cli binary")
	}
	if !executableFileExists(serverPath) {
		return fmt.Errorf("whisper.cpp runtime installation finished without whisper-server binary")
	}
	return nil
}

func executableFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func cxxCompilerPath() (string, error) {
	for _, name := range []string{"c++", "g++", "clang++"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("c++ compiler is required to install Whisper.cpp runtime")
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
