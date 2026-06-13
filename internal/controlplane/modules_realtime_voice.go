package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleRealtimeVoiceModule(ctx context.Context, args string) (Result, error) {
	if d.realtimeVoice == nil {
		return unsupportedRuntime("realtime voice"), nil
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.realtimeVoicePicker(ctx)
	case "enabled":
		return d.realtimeVoiceEnabledPicker(ctx)
	case "set-enabled":
		return d.setRealtimeVoiceEnabled(ctx, rest)
	case "provider", "provider-select":
		return d.realtimeVoiceProviderPicker(ctx)
	case "set-provider":
		return d.setRealtimeVoiceProvider(ctx, rest)
	case "setup", "provider-setup":
		return d.realtimeVoiceSetupPicker(ctx, rest)
	case "advanced":
		return d.realtimeVoiceAdvancedPicker(ctx, rest)
	case "voice":
		return d.realtimeVoiceVoicePicker(ctx, rest)
	case "model", "provider-model":
		return d.realtimeVoiceModelPicker(ctx, rest)
	case "language", "provider-language":
		return d.realtimeVoiceLanguagePicker(ctx, rest)
	case "setup-field", "provider-setup-field":
		return d.realtimeVoiceSetupField(ctx, rest)
	case "setup-set", "provider-setup-set":
		return d.realtimeVoiceSetupSet(ctx, rest)
	case "info", "status":
		return d.realtimeVoiceInfo(ctx)
	default:
		return d.realtimeVoicePicker(ctx)
	}
}

func (d *Dispatcher) realtimeVoicePicker(ctx context.Context) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerRealtimeVoice, module.Title).
			Context(module.ID).
			Back(modulesCommand()).
			Item(PickerItem{
				ID:       "provider",
				Title:    "Provider",
				Info:     realtimeVoiceProviderStatus(module),
				Command:  realtimeVoiceCommand("provider-select"),
				Selected: module.Enabled,
			}).
			Row("setup", "Provider Settings", realtimeVoiceSetupStatus(module), realtimeVoiceCommand("setup")).
			Row("status", "Status", module.Status, realtimeVoiceCommand("info")).
			Ptr(),
	}, nil
}

func (d *Dispatcher) realtimeVoiceEnabledPicker(ctx context.Context) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerRealtimeVoice, module.Title).
			Context(module.ID).
			Meta("Module is " + strings.ToLower(formatEnabled(module.Enabled))).
			Select(realtimeVoiceCommand()).
			Item(PickerItem{ID: "on", Title: "On", Info: realtimeVoiceEnableInfo(module), Selected: module.Enabled, Disabled: !realtimeVoiceModuleReady(module), Command: realtimeVoiceCommand("set-enabled", "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Info: module.Title, Selected: !module.Enabled, Command: realtimeVoiceCommand("set-enabled", "off")}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) setRealtimeVoiceEnabled(ctx context.Context, value string) (Result, error) {
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "on", "true", "enable", "enabled":
		enabled = true
	case "no", "off", "false", "disable", "disabled":
		enabled = false
	default:
		return d.realtimeVoiceEnabledPicker(ctx)
	}
	if enabled {
		module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
		if err != nil {
			return Result{}, err
		}
		if !realtimeVoiceModuleReady(module) {
			return d.realtimeVoiceSetupPicker(ctx, module.ProviderID)
		}
	}
	if _, err := d.realtimeVoice.UpdateRealtimeVoiceModule(ctx, setup.VoiceModuleUpdate{Enabled: &enabled}); err != nil {
		return Result{}, err
	}
	return d.realtimeVoicePicker(ctx)
}

func (d *Dispatcher) realtimeVoiceProviderPicker(ctx context.Context) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	picker := NewPickerData(PickerVoiceProvider, "Realtime Voice Provider").
		Context(module.ID).
		Select(realtimeVoiceCommand()).
		Item(PickerItem{
			ID:       "disabled",
			Title:    "Disabled",
			Selected: !module.Enabled,
			Command:  realtimeVoiceCommand("set-provider", "disabled"),
		})
	for _, provider := range module.Providers {
		picker.Item(PickerItem{
			ID:       provider.ID,
			Title:    provider.Name,
			Info:     realtimeVoiceProviderSelectionInfo(provider),
			Selected: module.Enabled && provider.ID == module.ProviderID,
			Command:  realtimeVoiceCommand("set-provider", provider.ID),
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) setRealtimeVoiceProvider(ctx context.Context, providerID string) (Result, error) {
	providerID = strings.TrimSpace(providerID)
	if strings.EqualFold(providerID, "disabled") || providerID == "" {
		enabled := false
		if _, err := d.realtimeVoice.UpdateRealtimeVoiceModule(ctx, setup.VoiceModuleUpdate{Enabled: &enabled}); err != nil {
			return Result{}, err
		}
		return d.realtimeVoicePicker(ctx)
	}
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	if !realtimeVoiceProviderExists(module, providerID) {
		return d.realtimeVoiceProviderPicker(ctx)
	}
	provider := realtimeVoiceProviderByID(module, providerID)
	enabled := realtimeVoiceProviderConfigured(provider)
	if _, err := d.realtimeVoice.UpdateRealtimeVoiceModule(ctx, setup.VoiceModuleUpdate{Enabled: &enabled, ProviderID: providerID}); err != nil {
		return Result{}, err
	}
	if !enabled {
		return d.realtimeVoiceSetupPicker(ctx, providerID)
	}
	return d.realtimeVoicePicker(ctx)
}

func (d *Dispatcher) realtimeVoiceSetupPicker(ctx context.Context, providerID string) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(providerID) == "" {
		return d.realtimeVoiceSetupProviderPicker(module), nil
	}
	provider := realtimeVoiceProviderForSetup(module, providerID)
	if strings.TrimSpace(provider.ID) == "" {
		return d.realtimeVoiceSetupProviderPicker(module), nil
	}
	picker := NewPickerData(PickerRealtimeVoice, provider.Name+" Setup").
		Context(module.ID).
		Back(realtimeVoiceCommand("setup")).
		Row("key", "API Key", realtimeVoiceAPIKeyStatus(provider), realtimeVoiceCommand("setup-field", "key", provider.ID)).
		Row("model", "Model", realtimeVoiceModelStatus(provider), realtimeVoiceCommand("model", provider.ID)).
		Row("voice", "Voice", realtimeVoiceVoiceStatus(provider), realtimeVoiceCommand("voice", provider.ID)).
		Row("language", "Language", realtimeVoiceLanguageStatus(provider, provider.Config.Language), realtimeVoiceCommand("language", provider.ID)).
		Row("status", "Status", realtimeVoiceProviderReadyStatus(provider), realtimeVoiceCommand("info")).
		Row("advanced", "Advanced", realtimeVoiceAdvancedStatus(provider), realtimeVoiceCommand("advanced", provider.ID))
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) realtimeVoiceSetupProviderPicker(module realtime.ModuleDescriptor) Result {
	picker := NewPickerData(PickerVoiceProvider, "Realtime Voice Provider").
		Context(module.ID).
		Back(realtimeVoiceCommand())
	for _, provider := range module.Providers {
		selected := module.Enabled && module.ProviderID == provider.ID
		info := realtimeVoiceProviderSelectionInfo(provider)
		if selected {
			info = strings.Join(nonEmptyStrings("Selected", info), " · ")
		}
		picker.Item(PickerItem{
			ID:       provider.ID,
			Title:    provider.Name,
			Info:     info,
			Selected: selected,
			Command:  realtimeVoiceCommand("setup", provider.ID),
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}
}

func (d *Dispatcher) realtimeVoiceModelPicker(ctx context.Context, providerID string) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	provider := realtimeVoiceProviderForSetup(module, providerID)
	if strings.TrimSpace(provider.ID) == "" {
		return d.realtimeVoiceSetupPicker(ctx, "")
	}
	current := strings.TrimSpace(provider.Config.ModelID)
	models := realtimeVoiceModelCandidates(provider)
	picker := NewPickerData(PickerVoiceProvider, provider.Name+" Model").
		Context(module.ID).
		Select(realtimeVoiceCommand("setup", provider.ID))
	if message := realtimeVoiceModelUnavailableMessage(provider, models); message != "" {
		picker.Item(PickerItem{
			ID:       "unavailable",
			Title:    "Model selection unavailable",
			Info:     message,
			Disabled: true,
		})
		return Result{Handled: true, Picker: picker.Ptr()}, nil
	}
	for _, modelID := range models {
		picker.Item(PickerItem{
			ID:       modelID,
			Title:    modelID,
			Selected: modelID == current,
			Command:  realtimeVoiceCommand("setup-set", "model", provider.ID, modelID),
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) realtimeVoiceVoicePicker(ctx context.Context, providerID string) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	provider := realtimeVoiceProviderForSetup(module, providerID)
	if strings.TrimSpace(provider.ID) == "" {
		return d.realtimeVoiceSetupPicker(ctx, "")
	}
	current := realtimeVoiceVoiceStatus(provider)
	voices := realtimeVoiceVoiceCandidates(provider)
	picker := NewPickerData(PickerVoiceProvider, provider.Name+" Voice").
		Context(module.ID).
		Select(realtimeVoiceCommand("setup", provider.ID))
	if len(voices) == 0 {
		picker.Item(PickerItem{
			ID:       "unavailable",
			Title:    "Voice selection unavailable",
			Info:     "No provider voices available",
			Disabled: true,
		})
		return Result{Handled: true, Picker: picker.Ptr()}, nil
	}
	for _, voiceID := range voices {
		picker.Item(PickerItem{
			ID:       voiceID,
			Title:    voiceID,
			Selected: strings.EqualFold(voiceID, current),
			Command:  realtimeVoiceCommand("setup-set", "voice", provider.ID, voiceID),
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) realtimeVoiceLanguagePicker(ctx context.Context, providerID string) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	provider := realtimeVoiceProviderForSetup(module, providerID)
	if strings.TrimSpace(provider.ID) == "" {
		return d.realtimeVoiceSetupPicker(ctx, "")
	}
	current := normalizeRealtimeVoiceLanguage(provider, provider.Config.Language)
	picker := NewPickerData(PickerVoiceProvider, provider.Name+" Language").
		Context(module.ID).
		Select(realtimeVoiceCommand("setup", provider.ID))
	for _, option := range realtimeVoiceLanguageOptions(provider) {
		picker.Item(PickerItem{
			ID:       option.id,
			Title:    option.title,
			Selected: option.id == current,
			Command:  realtimeVoiceCommand("setup-set", "language", provider.ID, option.id),
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) realtimeVoiceAdvancedPicker(ctx context.Context, providerID string) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	provider := realtimeVoiceProviderForSetup(module, providerID)
	if strings.TrimSpace(provider.ID) == "" {
		return d.realtimeVoiceSetupPicker(ctx, "")
	}
	cfg := provider.Config
	picker := NewPickerData(PickerRealtimeVoice, provider.Name+" Advanced").
		Context(module.ID).
		Back(realtimeVoiceCommand("setup", provider.ID)).
		Row("key-env", "API Key Env", realtimeVoiceAPIKeyEnvStatus(cfg.APIKeyEnv), realtimeVoiceCommand("setup-field", "key-env", provider.ID)).
		Row("endpoint", "Endpoint", realtimeVoiceEndpointStatus(cfg.Endpoint), realtimeVoiceCommand("setup-field", "endpoint", provider.ID))
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) realtimeVoiceSetupField(ctx context.Context, args string) (Result, error) {
	field, rest := firstCommandStep(args)
	providerID := firstField(rest)
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	provider := realtimeVoiceProviderForSetup(module, providerID)
	if strings.TrimSpace(provider.ID) == "" {
		return d.realtimeVoiceSetupPicker(ctx, "")
	}
	if strings.EqualFold(strings.TrimSpace(field), "model") || strings.EqualFold(strings.TrimSpace(field), "model-id") || strings.EqualFold(strings.TrimSpace(field), "model_id") {
		return d.realtimeVoiceModelPicker(ctx, provider.ID)
	}
	if strings.EqualFold(strings.TrimSpace(field), "voice") || strings.EqualFold(strings.TrimSpace(field), "voice-id") || strings.EqualFold(strings.TrimSpace(field), "voice_id") {
		return d.realtimeVoiceVoicePicker(ctx, provider.ID)
	}
	if strings.EqualFold(strings.TrimSpace(field), "language") || strings.EqualFold(strings.TrimSpace(field), "language-code") || strings.EqualFold(strings.TrimSpace(field), "language_code") {
		return d.realtimeVoiceLanguagePicker(ctx, provider.ID)
	}
	title, placeholder, value, sensitive := realtimeVoiceSetupPrompt(field, provider)
	if title == "" {
		return d.realtimeVoiceSetupPicker(ctx, provider.ID)
	}
	return Result{Handled: true, Prompt: &PromptData{
		Title:               title,
		Placeholder:         placeholder,
		Value:               value,
		SubmitCommandPrefix: realtimeVoiceCommand("setup-set", field, provider.ID) + " ",
		CancelCommand:       realtimeVoiceCommand("setup", provider.ID),
		Sensitive:           sensitive,
	}}, nil
}

func (d *Dispatcher) realtimeVoiceSetupSet(ctx context.Context, args string) (Result, error) {
	field, rest := firstCommandStep(args)
	providerID, value := firstCommandStep(rest)
	value = strings.TrimSpace(value)
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	provider := realtimeVoiceProviderForSetup(module, providerID)
	if strings.TrimSpace(provider.ID) == "" {
		return d.realtimeVoiceSetupPicker(ctx, "")
	}
	cfg := setup.VoiceProviderConfig{
		APIKeyEnv: provider.Config.APIKeyEnv,
		ModelID:   provider.Config.ModelID,
		VoiceID:   provider.Config.VoiceID,
		Language:  provider.Config.Language,
		Endpoint:  provider.Config.Endpoint,
	}
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "key", "api-key", "api_key":
		cfg.APIKey = value
	case "key-env", "api-key-env", "api_key_env":
		cfg.APIKeyEnv = value
	case "model", "model-id", "model_id":
		if value != "" && !stringInSliceFold(value, realtimeVoiceModelCandidates(provider)) {
			return d.realtimeVoiceModelPicker(ctx, provider.ID)
		}
		cfg.ModelID = value
	case "voice", "voice-id", "voice_id":
		if value != "" && !stringInSliceFold(value, realtimeVoiceVoiceCandidates(provider)) {
			return d.realtimeVoiceVoicePicker(ctx, provider.ID)
		}
		cfg.VoiceID = value
	case "language", "language-code", "language_code":
		if value != "" && !stringInSliceFold(value, realtimeVoiceLanguageCandidates(provider)) {
			return d.realtimeVoiceLanguagePicker(ctx, provider.ID)
		}
		cfg.Language = value
	case "endpoint", "url", "ws-url", "ws_url":
		cfg.Endpoint = value
	default:
		return d.realtimeVoiceSetupPicker(ctx, provider.ID)
	}
	if _, err := d.realtimeVoice.UpdateRealtimeVoiceModule(ctx, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg}); err != nil {
		return Result{}, err
	}
	return d.realtimeVoiceSetupPicker(ctx, provider.ID)
}

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

func realtimeVoiceSetupPrompt(field string, provider realtime.ProviderDescriptor) (title string, placeholder string, value string, sensitive bool) {
	cfg := provider.Config
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "key", "api-key", "api_key":
		return provider.Name + " API Key", realtimeVoiceAPIKeyPlaceholder(provider), "", true
	case "key-env", "api-key-env", "api_key_env":
		return provider.Name + " API Key Env", realtimeVoiceAPIKeyEnvPlaceholder(provider), cfg.APIKeyEnv, false
	case "model", "model-id", "model_id":
		return provider.Name + " Model", "select from provider catalog", cfg.ModelID, false
	case "voice", "voice-id", "voice_id":
		return provider.Name + " Voice", realtimeVoiceVoicePlaceholder(provider), cfg.VoiceID, false
	case "language", "language-code", "language_code":
		return provider.Name + " Language", realtimeVoiceLanguagePlaceholder(provider), cfg.Language, false
	case "endpoint", "url", "ws-url", "ws_url":
		return provider.Name + " Endpoint", realtimeVoiceEndpointPlaceholder(provider), cfg.Endpoint, false
	default:
		return "", "", "", false
	}
}

func realtimeVoiceModelCandidates(provider realtime.ProviderDescriptor) []string {
	out := make([]string, 0, len(provider.Models))
	seen := map[string]struct{}{}
	for _, value := range provider.Models {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func realtimeVoiceVoiceCandidates(provider realtime.ProviderDescriptor) []string {
	out := make([]string, 0, len(provider.Voices))
	seen := map[string]struct{}{}
	for _, value := range provider.Voices {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func realtimeVoiceLanguageCandidates(provider realtime.ProviderDescriptor) []string {
	options := realtimeVoiceLanguageOptions(provider)
	out := make([]string, 0, len(options))
	for _, option := range options {
		out = append(out, option.id)
	}
	return out
}

func realtimeVoiceLanguageOptions(provider realtime.ProviderDescriptor) []struct{ id, title string } {
	if provider.ID == realtime.ProviderGrok {
		return []struct{ id, title string }{
			{id: "auto", title: "Auto"},
			{id: "en", title: "English"},
			{id: "ar-EG", title: "Arabic (Egypt)"},
			{id: "ar-SA", title: "Arabic (Saudi Arabia)"},
			{id: "ar-AE", title: "Arabic (United Arab Emirates)"},
			{id: "bn", title: "Bengali"},
			{id: "zh", title: "Chinese"},
			{id: "fr", title: "French"},
			{id: "de", title: "German"},
			{id: "hi", title: "Hindi"},
			{id: "id", title: "Indonesian"},
			{id: "it", title: "Italian"},
			{id: "ja", title: "Japanese"},
			{id: "ko", title: "Korean"},
			{id: "pt-BR", title: "Portuguese (Brazil)"},
			{id: "pt-PT", title: "Portuguese (Portugal)"},
			{id: "ru", title: "Russian"},
			{id: "es-MX", title: "Spanish (Mexico)"},
			{id: "es-ES", title: "Spanish (Spain)"},
			{id: "tr", title: "Turkish"},
			{id: "vi", title: "Vietnamese"},
		}
	}
	return []struct{ id, title string }{
		{id: "auto", title: "Auto"},
		{id: "ar-EG", title: "Arabic (Egyptian)"},
		{id: "bn-BD", title: "Bengali (Bangladesh)"},
		{id: "nl-NL", title: "Dutch (Netherlands)"},
		{id: "en-IN", title: "English (India)"},
		{id: "en-US", title: "English (US)"},
		{id: "fr-FR", title: "French (France)"},
		{id: "de-DE", title: "German (Germany)"},
		{id: "hi-IN", title: "Hindi (India)"},
		{id: "id-ID", title: "Indonesian (Indonesia)"},
		{id: "it-IT", title: "Italian (Italy)"},
		{id: "ja-JP", title: "Japanese (Japan)"},
		{id: "ko-KR", title: "Korean (Korea)"},
		{id: "mr-IN", title: "Marathi (India)"},
		{id: "pl-PL", title: "Polish (Poland)"},
		{id: "pt-BR", title: "Portuguese (Brazil)"},
		{id: "ro-RO", title: "Romanian (Romania)"},
		{id: "ru-RU", title: "Russian (Russia)"},
		{id: "es-US", title: "Spanish (US)"},
		{id: "ta-IN", title: "Tamil (India)"},
		{id: "te-IN", title: "Telugu (India)"},
		{id: "th-TH", title: "Thai (Thailand)"},
		{id: "tr-TR", title: "Turkish (Turkey)"},
		{id: "uk-UA", title: "Ukrainian (Ukraine)"},
		{id: "vi-VN", title: "Vietnamese (Vietnam)"},
	}
}

func normalizeRealtimeVoiceLanguage(provider realtime.ProviderDescriptor, language string) string {
	if provider.ID == realtime.ProviderGrok {
		return normalizeGrokVoiceLanguage(language)
	}
	value := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	switch value {
	case "", "auto", "automatic", "detect", "default":
		return "auto"
	case "ar", "ar-eg":
		return "ar-EG"
	case "bn", "bn-bd":
		return "bn-BD"
	case "nl", "nl-nl":
		return "nl-NL"
	case "en", "en-us":
		return "en-US"
	case "en-in":
		return "en-IN"
	case "fr", "fr-fr":
		return "fr-FR"
	case "de", "de-de":
		return "de-DE"
	case "hi", "hi-in":
		return "hi-IN"
	case "id", "id-id":
		return "id-ID"
	case "it", "it-it":
		return "it-IT"
	case "ja", "ja-jp":
		return "ja-JP"
	case "ko", "ko-kr":
		return "ko-KR"
	case "mr", "mr-in":
		return "mr-IN"
	case "pl", "pl-pl":
		return "pl-PL"
	case "pt", "pt-br":
		return "pt-BR"
	case "ro", "ro-ro":
		return "ro-RO"
	case "ru", "ru-ru":
		return "ru-RU"
	case "es", "es-us":
		return "es-US"
	case "ta", "ta-in":
		return "ta-IN"
	case "te", "te-in":
		return "te-IN"
	case "th", "th-th":
		return "th-TH"
	case "tr", "tr-tr":
		return "tr-TR"
	case "uk", "uk-ua":
		return "uk-UA"
	case "vi", "vi-vn":
		return "vi-VN"
	default:
		return strings.TrimSpace(language)
	}
}

func normalizeGrokVoiceLanguage(language string) string {
	value := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	switch value {
	case "", "auto", "automatic", "detect", "default":
		return "auto"
	case "en", "en-us", "en-gb", "bn", "bn-bd", "zh", "zh-cn", "fr", "fr-fr", "de", "de-de", "hi", "hi-in", "id", "id-id", "it", "it-it", "ja", "ja-jp", "ko", "ko-kr", "ru", "ru-ru", "tr", "tr-tr", "vi", "vi-vn":
		if before, _, ok := strings.Cut(value, "-"); ok {
			return before
		}
		return value
	case "ar", "ar-eg":
		return "ar-EG"
	case "ar-sa":
		return "ar-SA"
	case "ar-ae":
		return "ar-AE"
	case "pt", "pt-br":
		return "pt-BR"
	case "pt-pt":
		return "pt-PT"
	case "es", "es-mx":
		return "es-MX"
	case "es-es":
		return "es-ES"
	default:
		return strings.TrimSpace(language)
	}
}

func realtimeVoiceAPIKeyPlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return firstNonEmptyTrimmed(provider.Config.APIKeyPreview, "xai-...")
	default:
		return firstNonEmptyTrimmed(provider.Config.APIKeyPreview, "AIza...")
	}
}

func realtimeVoiceAPIKeyEnvPlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return "XAI_API_KEY"
	default:
		return "MATRIXCLAW_GEMINI_LIVE_API_KEY"
	}
}

func realtimeVoiceVoicePlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return "eve"
	default:
		return "Puck"
	}
}

func realtimeVoiceLanguagePlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return "auto or ru"
	default:
		return "auto or ru-RU"
	}
}

func realtimeVoiceEndpointPlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return "wss://api.x.ai/v1/realtime"
	default:
		return "wss://generativelanguage.googleapis.com/..."
	}
}

func realtimeVoiceModelUnavailableMessage(provider realtime.ProviderDescriptor, models []string) string {
	if !provider.Config.APIKeyConfigured {
		return "API key required"
	}
	if !provider.Config.APIKeyValid {
		return firstNonEmptyTrimmed(provider.Status, "Invalid API key")
	}
	if len(models) == 0 {
		return firstNonEmptyTrimmed(provider.Status, "No realtime models available")
	}
	return ""
}

func stringInSliceFold(value string, values []string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, candidate := range values {
		if strings.EqualFold(value, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func realtimeVoiceAudioFormat(format realtime.AudioFormat) string {
	return fmt.Sprintf("%s · %d Hz · %d ch", format.Encoding, format.SampleRateHz, format.Channels)
}
