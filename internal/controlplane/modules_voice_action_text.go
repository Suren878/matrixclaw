package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func downloadActionInfo(provider setup.VoiceProviderOption) string {
	if voiceProviderDownloaded(provider) {
		return "Already installed"
	}
	for _, model := range provider.Models {
		selected := provider.Config.ModelID == model.ID || provider.Config.VoiceID == model.ID
		if selected {
			return strings.TrimSpace(strings.Join(nonEmptyStrings(model.Size, model.RAM), " · "))
		}
	}
	return "Install local files"
}

func deleteActionInfo(provider setup.VoiceProviderOption) string {
	if !voiceProviderDownloaded(provider) {
		return "Not Installed"
	}
	return "Remove local files"
}

func voiceRuntimeInstallAction(provider setup.VoiceProviderOption) string {
	if provider.RuntimeInstalled {
		return provider.ActionIDs.DeleteRuntime
	}
	return provider.ActionIDs.InstallRuntime
}

func piperRuntimeActionRole(provider setup.VoiceProviderOption, action string) PickerItemRole {
	if action == provider.ActionIDs.DeleteRuntime {
		return PickerItemRoleDanger
	}
	return PickerItemRoleAction
}

func voiceRuntimeInstallInfo(provider setup.VoiceProviderOption) string {
	if provider.RuntimeInstalled {
		return "Installed"
	}
	if provider.ID == "whispercpp" {
		return "Not Installed · Builds Locally"
	}
	return "Not Installed"
}

func voiceRuntimeInstallConfirmMessage(provider setup.VoiceProviderOption) string {
	if provider.ID == "whispercpp" {
		return "Build " + provider.Name + " engine?"
	}
	return "Download " + provider.Name + " engine?"
}

func voiceRuntimeInstallConfirmLabel(provider setup.VoiceProviderOption) string {
	if provider.ID == "whispercpp" {
		return "Build"
	}
	return "Download"
}

func voiceModelInstallWithRuntimeMessage(provider setup.VoiceProviderOption, modelID string) string {
	name := voiceModelName(provider, modelID)
	if provider.ID == "whispercpp" {
		return "Build Whisper.cpp engine and download `" + name + "` model?"
	}
	return "Download `" + name + "`?"
}

func supertonicRuntimeInstallInfo(provider setup.VoiceProviderOption) string {
	if provider.RuntimeInstalled {
		return "Installed"
	}
	return "Not Installed"
}

func voiceRuntimeConfirmMessage(provider setup.VoiceProviderOption, action string) string {
	if strings.TrimSpace(action) == strings.TrimSpace(provider.ActionIDs.Stop) {
		return "Stop " + provider.Name + " runtime?"
	}
	return "Start " + provider.Name + " runtime?"
}

func voiceRuntimeDeleteConfirmMessage(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	if module.ID == setup.VoiceModuleTTS && provider.ID == "piper" {
		return "Delete Piper engine and installed voices?"
	}
	return "Delete " + provider.Name + " engine?"
}

func voiceRuntimeConfirmLabel(provider setup.VoiceProviderOption, action string) string {
	if strings.TrimSpace(action) == strings.TrimSpace(provider.ActionIDs.Stop) {
		return "Stop"
	}
	return "Start"
}

func voiceDownloadedMessage(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, modelID string) string {
	kind := "Model"
	name := voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.ModelID))
	if module.ID == setup.VoiceModuleTTS {
		kind = "Voice"
		name = voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.VoiceID))
	}
	return kind + " `" + name + "` installed. Runtime start/stop is separate from local files."
}

func voiceDeletedMessage(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, modelID string) string {
	kind := "Model"
	name := voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.ModelID))
	if module.ID == setup.VoiceModuleTTS {
		kind = "Voice"
		name = voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.VoiceID))
	}
	return kind + " `" + name + "` files deleted. The provider can stay selected, but it cannot run until the files are installed again."
}

func confirmLabelAfterDownload(moduleID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return "Voices"
	}
	return "Status"
}

func voiceCommandAfterDownload(moduleID string, providerID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return voiceModuleCommand(moduleID, "provider-installed", providerID)
	}
	return voiceModuleCommand(moduleID, "provider-status", providerID)
}

func confirmLabelAfterDelete(moduleID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return "Voices"
	}
	return "Status"
}

func voiceCommandAfterDelete(moduleID string, providerID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return voiceModuleCommand(moduleID, "provider-installed", providerID)
	}
	return voiceModuleCommand(moduleID, "provider-status", providerID)
}

func voiceCancelCommandAfterDelete(moduleID string, providerID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return voiceModuleCommand(moduleID, "provider-installed", providerID)
	}
	return voiceModuleCommand(moduleID, "provider", providerID)
}

func voiceDeleteConfirmMessage(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, modelID string) string {
	if module.ID == setup.VoiceModuleTTS {
		return "Delete voice `" + voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.VoiceID)) + "`?"
	}
	return "Delete local model for `" + provider.Name + "`?"
}
