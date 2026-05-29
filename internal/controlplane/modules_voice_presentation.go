package controlplane

import "github.com/Suren878/matrixclaw/internal/setup"

type voiceLocalProviderPresentation struct {
	RuntimeAction string
	RuntimeInfo   string
	RuntimeRole   PickerItemRole
	RunMode       string
	SharedRuntime bool
}

func voiceLocalProviderPresentationFor(provider setup.VoiceProviderOption) voiceLocalProviderPresentation {
	runtimeAction := voiceRuntimeInstallAction(provider)
	return voiceLocalProviderPresentation{
		RuntimeAction: runtimeAction,
		RuntimeInfo:   voiceRuntimeInstallInfo(provider),
		RuntimeRole:   piperRuntimeActionRole(provider, runtimeAction),
		RunMode:       voiceRunModeLabel(provider),
		SharedRuntime: provider.ID == "supertonic",
	}
}

func voiceLocalRuntimeEngineItem(moduleID string, provider setup.VoiceProviderOption, presentation voiceLocalProviderPresentation) PickerItem {
	return PickerItem{
		ID:      "engine",
		Title:   "Engine",
		Info:    presentation.RuntimeInfo,
		Command: voiceModuleCommand(moduleID, "provider-action", provider.ID, presentation.RuntimeAction),
		Role:    presentation.RuntimeRole,
	}
}

func voiceLocalRunModeItem(moduleID string, provider setup.VoiceProviderOption, presentation voiceLocalProviderPresentation) PickerItem {
	return PickerItem{
		ID:      "run-mode",
		Title:   "Run Mode",
		Info:    presentation.RunMode,
		Command: voiceModuleCommand(moduleID, "provider-run-mode", provider.ID),
	}
}
