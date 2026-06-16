package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

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
