package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

func (d *Dispatcher) realtimeVoiceInfo(ctx context.Context) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	provider := realtimeVoiceProvider(module)
	return Result{
		Handled: true,
		Info: &InfoData{
			Title: module.Title + " Status",
			Rows: []InfoRow{
				{Label: "Enabled", Value: formatEnabled(module.Enabled)},
				{Label: "Provider", Value: firstNonEmptyTrimmed(module.ProviderName, module.ProviderID)},
				{Label: "Provider status", Value: firstNonEmptyTrimmed(provider.Status, module.Status)},
				{Label: "API key", Value: realtimeVoiceAPIKeyStatus(provider)},
				{Label: "Model", Value: firstNonEmptyTrimmed(module.ModelID, provider.Config.ModelID, "Not selected")},
				{Label: "Voice", Value: firstNonEmptyTrimmed(module.Config.VoiceID, provider.Config.VoiceID)},
				{Label: "Language", Value: realtimeVoiceLanguageStatus(provider, firstNonEmptyTrimmed(module.Config.Language, provider.Config.Language))},
				{Label: "Endpoint", Value: realtimeVoiceEndpointStatus(firstNonEmptyTrimmed(module.Config.Endpoint, provider.Config.Endpoint))},
				{Label: "Input", Value: realtimeVoiceAudioFormat(module.InputAudio)},
				{Label: "Output", Value: realtimeVoiceAudioFormat(module.OutputAudio)},
			},
		},
	}, nil
}

func realtimeVoiceModuleListInfo(module realtime.ModuleDescriptor) string {
	if !module.Enabled {
		return ""
	}
	return firstNonEmptyTrimmed(module.ProviderName, module.ProviderID)
}

func realtimeVoiceProviderStatus(module realtime.ModuleDescriptor) string {
	provider := realtimeVoiceProvider(module)
	if !module.Enabled && realtimeVoiceProviderConfigured(provider) {
		return "Disabled"
	}
	if !module.Enabled {
		return firstNonEmptyTrimmed(provider.Status, realtimeVoiceConfiguredLabel(realtimeVoiceProviderConfigured(provider)))
	}
	parts := nonEmptyStrings(firstNonEmptyTrimmed(provider.Name, module.ProviderName, module.ProviderID), realtimeVoiceProviderReadyStatus(provider))
	return strings.Join(parts, " · ")
}

func realtimeVoiceProviderSelectionInfo(provider realtime.ProviderDescriptor) string {
	return firstNonEmptyTrimmed(provider.Status, realtimeVoiceProviderReadyStatus(provider))
}

func realtimeVoiceProvider(module realtime.ModuleDescriptor) realtime.ProviderDescriptor {
	for _, provider := range module.Providers {
		if provider.ID == module.ProviderID {
			return provider
		}
	}
	if len(module.Providers) > 0 {
		return module.Providers[0]
	}
	return realtime.ProviderDescriptor{}
}

func realtimeVoiceProviderByID(module realtime.ModuleDescriptor, providerID string) realtime.ProviderDescriptor {
	for _, provider := range module.Providers {
		if provider.ID == providerID {
			return provider
		}
	}
	return realtime.ProviderDescriptor{}
}

func realtimeVoiceProviderForSetup(module realtime.ModuleDescriptor, providerID string) realtime.ProviderDescriptor {
	providerID = strings.TrimSpace(providerID)
	if providerID != "" {
		return realtimeVoiceProviderByID(module, providerID)
	}
	return realtimeVoiceProvider(module)
}

func realtimeVoiceProviderExists(module realtime.ModuleDescriptor, providerID string) bool {
	for _, provider := range module.Providers {
		if provider.ID == providerID {
			return true
		}
	}
	return false
}

func realtimeVoiceModuleReady(module realtime.ModuleDescriptor) bool {
	return realtimeVoiceProviderConfigured(realtimeVoiceProvider(module))
}

func realtimeVoiceProviderConfigured(provider realtime.ProviderDescriptor) bool {
	return provider.Configured
}

func realtimeVoiceConfiguredLabel(configured bool) string {
	if configured {
		return "Configured"
	}
	return "API key required"
}

func realtimeVoiceProviderReadyStatus(provider realtime.ProviderDescriptor) string {
	if status := strings.TrimSpace(provider.Status); status != "" {
		return status
	}
	if realtimeVoiceProviderConfigured(provider) {
		return "Ready"
	}
	return "API key required"
}

func realtimeVoiceSetupStatus(module realtime.ModuleDescriptor) string {
	provider := realtimeVoiceProvider(module)
	if strings.TrimSpace(provider.Name) == "" {
		return ""
	}
	return strings.Join(nonEmptyStrings(provider.Name, realtimeVoiceAPIKeyStatus(provider), realtimeVoiceModelStatus(provider)), " · ")
}

func realtimeVoiceEnableInfo(module realtime.ModuleDescriptor) string {
	provider := realtimeVoiceProvider(module)
	if realtimeVoiceProviderConfigured(provider) {
		return firstNonEmptyTrimmed(provider.Name, module.ProviderName)
	}
	return firstNonEmptyTrimmed(provider.Status, "API key required")
}

func realtimeVoiceAPIKeyStatus(provider realtime.ProviderDescriptor) string {
	preview := strings.TrimSpace(provider.Config.APIKeyPreview)
	if provider.Config.APIKeyConfigured {
		if provider.Config.APIKeyValid {
			return firstNonEmptyTrimmed(preview, "Verified")
		}
		if preview != "" {
			return firstNonEmptyTrimmed(provider.Status, "Invalid API key") + " (" + preview + ")"
		}
		return firstNonEmptyTrimmed(provider.Status, "Invalid API key")
	}
	return "API key required"
}

func realtimeVoiceModelStatus(provider realtime.ProviderDescriptor) string {
	if modelID := strings.TrimSpace(provider.Config.ModelID); modelID != "" {
		return modelID
	}
	if !provider.Config.APIKeyConfigured {
		return "API key required"
	}
	if !provider.Config.APIKeyValid {
		return "Key not verified"
	}
	if len(provider.Models) == 0 {
		return "No realtime models"
	}
	return "Select model"
}

func realtimeVoiceVoiceStatus(provider realtime.ProviderDescriptor) string {
	if voiceID := strings.TrimSpace(provider.Config.VoiceID); voiceID != "" {
		return voiceID
	}
	if len(provider.Voices) > 0 {
		return firstNonEmptyTrimmed(provider.Voices[0], "Select voice")
	}
	return "No voices"
}

func realtimeVoiceLanguageStatus(provider realtime.ProviderDescriptor, language string) string {
	code := normalizeRealtimeVoiceLanguage(provider, language)
	for _, option := range realtimeVoiceLanguageOptions(provider) {
		if option.id == code {
			return option.title
		}
	}
	return firstNonEmptyTrimmed(strings.TrimSpace(language), "Auto")
}

func realtimeVoiceAPIKeyEnvStatus(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "Default env fallbacks"
}

func realtimeVoiceEndpointStatus(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "Default endpoint"
}

func realtimeVoiceAdvancedStatus(provider realtime.ProviderDescriptor) string {
	parts := []string{}
	if strings.TrimSpace(provider.Config.APIKeyEnv) != "" {
		parts = append(parts, "env")
	}
	if strings.TrimSpace(provider.Config.Endpoint) != "" {
		parts = append(parts, "endpoint")
	}
	if len(parts) == 0 {
		return "Defaults"
	}
	return strings.Join(parts, " · ")
}

func realtimeVoiceAudioFormat(format realtime.AudioFormat) string {
	return fmt.Sprintf("%s · %d Hz · %d ch", format.Encoding, format.SampleRateHz, format.Channels)
}
