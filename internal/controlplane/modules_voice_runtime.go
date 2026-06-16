package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func voiceProviderDownloaded(provider setup.VoiceProviderOption) bool {
	return provider.Downloaded
}

func voiceRuntimeState(provider setup.VoiceProviderOption) string {
	state := strings.ToLower(strings.TrimSpace(provider.RuntimeState))
	if state != "" {
		return state
	}
	if !voiceProviderDownloaded(provider) {
		return "unavailable"
	}
	if provider.Local {
		return "not_implemented"
	}
	return "stopped"
}

func voiceDownloadState(provider setup.VoiceProviderOption) string {
	if voiceProviderDownloaded(provider) {
		return "Installed"
	}
	return "Not Installed"
}

func voiceRuntimeStateLabel(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running":
		return "Running"
	case "starting":
		return "Starting"
	case "error":
		return "Error"
	case "stopped":
		return "Not running"
	case "not_implemented", "unsupported":
		return "Not implemented yet"
	default:
		return "Not available"
	}
}

func voiceRuntimeManagerInfo(provider setup.VoiceProviderOption) string {
	if voiceRunModePerTaskSelected(provider) {
		return "Run per task"
	}
	state := voiceRuntimeState(provider)
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "not_implemented", "unsupported":
		return "Not implemented yet"
	case "unavailable":
		if voiceProviderDownloaded(provider) {
			return "Not implemented yet"
		}
		return "Not available"
	}
	return voiceRuntimeStateLabel(state)
}

const (
	voiceRuntimeModePerTask = "per_task"
	voiceRuntimeModeAlways  = "always_running"
)

func normalizeVoiceRunMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "always_running", "persistent", "server":
		return voiceRuntimeModeAlways
	default:
		return voiceRuntimeModePerTask
	}
}

func voiceRunModeLabel(provider setup.VoiceProviderOption) string {
	if voiceRunModeAlways(provider) {
		return "Always running"
	}
	return "Run per task"
}

func voiceRunPerTaskTitle(provider setup.VoiceProviderOption) string {
	switch provider.ID {
	case "piper":
		return "Run Per Task (~1.4s)"
	case "supertonic":
		return "Run Per Task (~1.2s)"
	default:
		return "Run Per Task"
	}
}

func voiceRunModeAlways(provider setup.VoiceProviderOption) bool {
	return normalizeVoiceRunMode(provider.Config.RuntimeMode) == voiceRuntimeModeAlways && voicePersistentRuntimeAvailable(provider)
}

func voiceRunModePerTaskSelected(provider setup.VoiceProviderOption) bool {
	return !voiceRunModeAlways(provider)
}

func voicePersistentRuntimeAvailable(provider setup.VoiceProviderOption) bool {
	return provider.ID == "piper" || provider.ID == "whispercpp" || provider.ID == "supertonic"
}

func voicePersistentProvider(moduleID string, providerID string) bool {
	switch moduleID {
	case setup.VoiceModuleTTS:
		return providerID == "piper" || providerID == "supertonic"
	case setup.VoiceModuleSTT:
		return providerID == "whispercpp"
	default:
		return false
	}
}

func persistentRuntimeRAMEstimate(provider setup.VoiceProviderOption) string {
	switch provider.ID {
	case "piper":
		return "≈130 MB RAM"
	case "supertonic":
		return "≈550 MB RAM"
	case "whispercpp":
		return "Model RAM"
	default:
		return ""
	}
}

func voiceRuntimeRAMStatus(provider setup.VoiceProviderOption) string {
	if provider.RuntimeRSS == 0 {
		return "0 B"
	}
	return formatBytes(provider.RuntimeRSS)
}
