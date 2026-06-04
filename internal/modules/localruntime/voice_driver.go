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

type voiceProviderDriver interface {
	decorate(r *Runtime, moduleID string, provider setup.VoiceProviderOption) setup.VoiceProviderOption
	actionIDs() setup.VoiceProviderActionIDs
	modelPath(r *Runtime, moduleID string, provider setup.VoiceProviderOption, modelID string) string
	modelInstalled(path string) bool
	downloads(provider setup.VoiceProviderOption, modelPath string) ([]downloadItem, error)
	actionTarget(provider setup.VoiceProviderOption, modelID string) setup.VoiceProviderOption
	installRuntime(ctx context.Context, r *Runtime, moduleID string, provider setup.VoiceProviderOption) error
	deleteRuntime(r *Runtime, moduleID string, provider setup.VoiceProviderOption) error
	startRuntime(ctx context.Context, r *Runtime, moduleID string, provider setup.VoiceProviderOption) error
	stopRuntime(r *Runtime, moduleID string, provider setup.VoiceProviderOption) error
	stopForDelete(r *Runtime, provider setup.VoiceProviderOption) error
	runtimeRunning(r *Runtime, provider setup.VoiceProviderOption) bool
	managedBinaryPath(r *Runtime, provider setup.VoiceProviderOption) (string, error)
	voiceBinaryPath(r *Runtime, provider setup.VoiceProviderOption) (string, error)
	processNames(provider setup.VoiceProviderOption) []string
}

func driverForProvider(providerID string) (voiceProviderDriver, bool) {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "piper":
		return piperDriver{}, true
	case "supertonic":
		return supertonicDriver{}, true
	case "whispercpp":
		return whisperDriver{}, true
	default:
		return nil, false
	}
}

func voiceProviderActionIDsWithModels() setup.VoiceProviderActionIDs {
	return setup.VoiceProviderActionIDs{
		InstallRuntime:           ActionInstallRuntime,
		DeleteRuntime:            ActionDeleteRuntime,
		DownloadModel:            ActionDownload,
		DownloadModelWithRuntime: "download-with-runtime",
		DeleteModel:              ActionDelete,
		Start:                    ActionStart,
		Stop:                     ActionStop,
	}
}

func voiceProviderActionIDsRuntimeOnly() setup.VoiceProviderActionIDs {
	return setup.VoiceProviderActionIDs{
		InstallRuntime: ActionInstallRuntime,
		DeleteRuntime:  ActionDeleteRuntime,
		Start:          ActionStart,
		Stop:           ActionStop,
	}
}

func decorateGenericLocalVoiceProvider(r *Runtime, moduleID string, provider setup.VoiceProviderOption) setup.VoiceProviderOption {
	provider = r.decorateVoiceModels(moduleID, provider)
	provider.RuntimeRSS = r.VoiceRuntimeRSSBytes(provider)
	if path, err := r.ManagedVoiceBinaryPath(provider); err == nil {
		provider.RuntimeInstalled = true
		provider.RuntimePath = path
	}
	installed, path := r.VoiceModelInstalled(moduleID, provider)
	provider.Downloaded = installed
	provider.ModelPath = path
	provider.Endpoint = path
	installedCount := installedVoiceModelCount(provider.Models)
	if installed {
		provider.RuntimeState = RuntimeNotImplemented
		provider.RuntimeDetail = "Runtime process management is not implemented yet"
		provider.Status = "Local · installed · runtime manager unavailable"
		return provider
	}
	provider.RuntimeState = RuntimeUnavailable
	provider.RuntimeDetail = "Download the selected local files before local voice can run"
	if installedCount > 0 {
		provider.Status = fmt.Sprintf("Local · active voice not installed · %d installed", installedCount)
	} else {
		provider.Status = "Local · not installed"
	}
	return provider
}

func decorateProviderModelRuntime(r *Runtime, moduleID string, provider setup.VoiceProviderOption, runtimeMissingDetail string) setup.VoiceProviderOption {
	provider = r.decorateVoiceModels(moduleID, provider)
	provider.RuntimeRSS = r.VoiceRuntimeRSSBytes(provider)
	if path, err := r.ManagedVoiceBinaryPath(provider); err == nil {
		provider.RuntimeInstalled = true
		provider.RuntimePath = path
	}
	installed, path := r.VoiceModelInstalled(moduleID, provider)
	provider.Downloaded = installed
	provider.ModelPath = path
	provider.Endpoint = path
	installedCount := installedVoiceModelCount(provider.Models)
	if !installed {
		provider.RuntimeState = RuntimeUnavailable
		provider.RuntimeDetail = "Download the selected local files before local voice can run"
		if installedCount > 0 {
			provider.Status = fmt.Sprintf("Local · active voice not installed · %d installed", installedCount)
		} else {
			provider.Status = "Local · not installed"
		}
		return provider
	}
	if updated, unavailable := markProviderRuntimeUnavailable(provider, runtimeMissingDetail); unavailable {
		provider = updated
		return provider
	}
	if voiceProviderRunsPerTask(provider) {
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
	return provider
}

func markProviderRuntimeUnavailable(provider setup.VoiceProviderOption, detail string) (setup.VoiceProviderOption, bool) {
	if strings.TrimSpace(detail) == "" {
		return provider, false
	}
	provider.RuntimeState = RuntimeUnavailable
	provider.RuntimeDetail = detail
	provider.Status = "Local · runtime missing"
	return provider, true
}

func localVoiceBinaryPath(provider setup.VoiceProviderOption, runtimeInstalled func() bool, managed func() string) (string, error) {
	if runtimeInstalled != nil && !runtimeInstalled() {
		return "", fmt.Errorf("%s runtime is not installed", provider.Name)
	}
	if path, err := localManagedBinaryPath(provider, managed); err == nil {
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

func localManagedBinaryPath(provider setup.VoiceProviderOption, managed func() string) (string, error) {
	value := strings.TrimSpace(provider.Config.BinaryPath)
	if filepath.IsAbs(value) || strings.Contains(value, string(os.PathSeparator)) {
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value, nil
		}
	}
	if managed != nil {
		path := managed()
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s runtime is not installed", provider.Name)
}

func configuredBinaryProcessName(provider setup.VoiceProviderOption) []string {
	if binary := strings.TrimSpace(provider.Config.BinaryPath); binary != "" {
		return []string{filepath.Base(binary)}
	}
	return nil
}
