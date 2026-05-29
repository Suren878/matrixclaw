package localruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

type piperDriver struct{}

func (piperDriver) actionIDs() setup.VoiceProviderActionIDs {
	return voiceProviderActionIDsWithModels()
}

func (piperDriver) decorate(r *Runtime, moduleID string, provider setup.VoiceProviderOption) setup.VoiceProviderOption {
	if models, ok := piperCatalogModels(); ok && len(models) > 0 {
		provider.Models = models
		provider.CatalogStatus = "online"
		provider.CatalogDetail = fmt.Sprintf("%d voices", len(models))
	} else {
		provider.CatalogStatus = "fallback"
		provider.CatalogDetail = "using bundled fallback voices"
	}
	provider = ensureConfiguredPiperVoiceModel(provider)
	runtimeDetail := ""
	if _, err := r.VoiceBinaryPath(provider); err != nil {
		runtimeDetail = "Piper runtime is not installed"
	}
	return decorateProviderModelRuntime(r, moduleID, provider, runtimeDetail)
}

func (piperDriver) modelPath(r *Runtime, _ string, provider setup.VoiceProviderOption, modelID string) string {
	voiceID := strings.TrimSpace(modelID)
	if voiceID == "" {
		voiceID = strings.TrimSpace(provider.Config.VoiceID)
	}
	if voiceID == "" {
		voiceID = "en_US-lessac-medium"
	}
	return filepath.Join(r.rootDir(), "voice", "tts", "piper", voiceID, voiceID+".onnx")
}

func (piperDriver) modelInstalled(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	info, err = os.Stat(path + ".json")
	return err == nil && !info.IsDir() && info.Size() > 0
}

func (piperDriver) downloads(provider setup.VoiceProviderOption, modelPath string) ([]downloadItem, error) {
	url, err := piperVoiceURL(provider.Config.VoiceID)
	if err != nil {
		return nil, err
	}
	return []downloadItem{
		{URL: url + ".onnx", Path: modelPath},
		{URL: url + ".onnx.json", Path: modelPath + ".json"},
	}, nil
}

func (piperDriver) actionTarget(provider setup.VoiceProviderOption, modelID string) setup.VoiceProviderOption {
	if modelID = strings.TrimSpace(modelID); modelID != "" {
		provider.Config.VoiceID = modelID
	}
	return provider
}

func (piperDriver) installRuntime(ctx context.Context, r *Runtime, _ string, _ setup.VoiceProviderOption) error {
	return r.installPiperRuntime(ctx)
}

func (piperDriver) deleteRuntime(r *Runtime, _ string, provider setup.VoiceProviderOption) error {
	if err := r.stopPiperProcess(provider); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Dir(filepath.Dir(r.managedPiperBinaryPath()))); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(r.rootDir(), "voice", "tts", "piper"))
}

func (piperDriver) startRuntime(ctx context.Context, r *Runtime, moduleID string, provider setup.VoiceProviderOption) error {
	if _, err := r.VoiceBinaryPath(provider); err != nil {
		return err
	}
	return r.startPiperProcess(moduleID, provider)
}

func (piperDriver) stopRuntime(r *Runtime, _ string, provider setup.VoiceProviderOption) error {
	return r.stopPiperProcess(provider)
}

func (piperDriver) stopForDelete(r *Runtime, provider setup.VoiceProviderOption) error {
	return r.stopPiperProcess(provider)
}

func (piperDriver) runtimeRunning(r *Runtime, provider setup.VoiceProviderOption) bool {
	return r.piperProcessRunning(provider)
}

func (piperDriver) managedBinaryPath(r *Runtime, provider setup.VoiceProviderOption) (string, error) {
	return localManagedBinaryPath(provider, r.managedPiperBinaryPath)
}

func (piperDriver) voiceBinaryPath(r *Runtime, provider setup.VoiceProviderOption) (string, error) {
	return localVoiceBinaryPath(r, provider, nil, r.managedPiperBinaryPath)
}

func (piperDriver) processNames(provider setup.VoiceProviderOption) []string {
	return append(configuredBinaryProcessName(provider), "piper", "piper-tts")
}

func (r *Runtime) managedPiperBinaryPath() string {
	return filepath.Join(r.runtimeDir(), "piper-venv", "bin", "piper")
}

func (r *Runtime) managedPiperPythonPath() string {
	return filepath.Join(r.runtimeDir(), "piper-venv", "bin", "python")
}

func (r *Runtime) installPiperRuntime(ctx context.Context) error {
	if err := requireFFmpegForLocalTTS(); err != nil {
		return err
	}
	if info, err := os.Stat(r.managedPiperBinaryPath()); err == nil && !info.IsDir() {
		return nil
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil {
		return fmt.Errorf("python 3 is required to install Piper runtime")
	}
	venvDir := filepath.Dir(filepath.Dir(r.managedPiperBinaryPath()))
	if err := os.RemoveAll(venvDir); err != nil {
		return err
	}
	if err := os.MkdirAll(r.runtimeDir(), 0o755); err != nil {
		return err
	}
	if err := runRuntimeCommand(ctx, python, "-m", "venv", venvDir); err != nil {
		return err
	}
	venvPython := r.managedPiperPythonPath()
	if err := runRuntimeCommand(ctx, venvPython, "-m", "pip", "install", "--upgrade", "pip"); err != nil {
		return err
	}
	if err := runRuntimeCommand(ctx, venvPython, "-m", "pip", "install", "piper-tts"); err != nil {
		return err
	}
	if info, err := os.Stat(r.managedPiperBinaryPath()); err != nil || info.IsDir() {
		return fmt.Errorf("piper runtime installation finished without piper binary")
	}
	return nil
}

func ensureConfiguredPiperVoiceModel(provider setup.VoiceProviderOption) setup.VoiceProviderOption {
	voiceID := strings.TrimSpace(provider.Config.VoiceID)
	if voiceID == "" {
		return provider
	}
	for _, model := range provider.Models {
		if strings.EqualFold(model.ID, voiceID) {
			return provider
		}
	}
	model := setup.VoiceModelOption{
		ID:   voiceID,
		Name: voiceID,
	}
	if language, voice, quality, ok := splitPiperVoiceID(voiceID); ok {
		model.Name = strings.TrimSpace(strings.Join(nonEmptyLocal(titleWords(voice), qualityLabel(quality)), " "))
		model.LanguageCode = language
		model.Quality = quality
		if model.Name == "" {
			model.Name = voiceID
		}
	}
	provider.Models = append(provider.Models, model)
	return provider
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
