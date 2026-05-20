package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) voiceModuleProviderFormFromToken(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, rest)
	if err != nil {
		return Result{}, err
	}
	return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, data, "")
}

func (d *Dispatcher) voiceModuleProviderField(ctx context.Context, moduleID string, args string) (Result, error) {
	field, rest := firstCommandStep(args)
	providerID, rest := firstCommandToken(rest)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, rest)
	if err != nil {
		return Result{}, err
	}
	title := voiceProviderFormTitle(moduleID, providerID, provider)
	switch field {
	case "base":
		if providerEditBaseURLUsesPicker(provider, data) {
			token := encodeCustomProviderFormToken(data)
			return customProviderBaseURLPicker(
				title,
				data,
				providerFormCatalogID(provider),
				voiceModuleCommandPrefix(moduleID, "provider-set", "base", providerID, token),
				voiceModuleCommand(moduleID, "provider-form", providerID, token),
			), nil
		}
	case "model":
		if providerEditModelUsesPicker(provider, data) {
			return d.voiceProviderModelPicker(ctx, moduleID, providerID, provider, data), nil
		}
	}
	token := encodeCustomProviderFormToken(data)
	placeholder := ""
	if field == "key" {
		placeholder = "leave empty to keep"
	}
	return customProviderFieldPrompt(
		title,
		field,
		data,
		placeholder,
		voiceModuleCommandPrefix(moduleID, "provider-set", field, providerID, token),
		voiceModuleCommand(moduleID, "provider-form", providerID, token),
	), nil
}

func (d *Dispatcher) voiceModuleProviderSet(ctx context.Context, moduleID string, args string) (Result, error) {
	field, rest := firstCommandStep(args)
	providerID, rest := firstCommandToken(rest)
	token := firstField(rest)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, token)
	if err != nil {
		return Result{}, err
	}
	data = data.WithField(field, strings.TrimSpace(strings.TrimPrefix(rest, token)))
	return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, data, "")
}

func (d *Dispatcher) saveVoiceModuleProvider(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, rest)
	if err != nil {
		return Result{}, err
	}
	if message := providerEditValidationMessage(provider, data); message != "" {
		return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, data, message)
	}
	if _, err := d.providers.ConfigureSetupProvider(ctx, provider.ID, providerUpdateFromForm(provider, data, false)); err != nil {
		return Result{}, err
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, moduleID, setup.VoiceModuleUpdate{ProviderID: providerID}); err != nil {
		return Result{}, err
	}
	return d.voiceModuleProviderPicker(ctx, moduleID)
}

func (d *Dispatcher) voiceProviderModelPicker(ctx context.Context, moduleID string, providerID string, provider setup.ProviderSetupItem, data setup.ProviderFormState) Result {
	token := encodeCustomProviderFormToken(data)
	models, err := d.providers.ProviderModels(ctx, provider.ID, providerUpdateFromForm(provider, data, false))
	if err != nil {
		return customProviderFieldPrompt(
			voiceProviderFormTitle(moduleID, providerID, provider),
			"model",
			data,
			"Could not load remote models: "+err.Error()+". Enter the model manually.",
			voiceModuleCommandPrefix(moduleID, "provider-set", "model", providerID, token),
			voiceModuleCommand(moduleID, "provider-form", providerID, token),
		)
	}
	current := strings.TrimSpace(data.Model)
	items := make([]PickerItem, 0, len(models))
	for _, modelID := range models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		items = append(items, PickerItem{
			ID:       modelID,
			Title:    modelID,
			Selected: modelID == current,
			Command:  voiceModuleCommand(moduleID, "provider-set", "model", providerID, token, modelID),
		})
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, "Model").
			Back(voiceModuleCommand(moduleID, "provider-form", providerID, token)).
			Items(items...).
			Ptr(),
	}
}

func (d *Dispatcher) voiceProviderFormData(ctx context.Context, providerID string, token string) (setup.ProviderSetupItem, setup.ProviderFormState, error) {
	provider, err := d.voiceSetupProvider(ctx, providerID)
	if err != nil {
		return setup.ProviderSetupItem{}, setup.ProviderFormState{}, err
	}
	data, err := decodeCustomProviderFormToken(firstField(token))
	if err != nil {
		return setup.ProviderSetupItem{}, setup.ProviderFormState{}, err
	}
	return provider, data, nil
}

func (d *Dispatcher) voiceSetupProvider(ctx context.Context, providerID string) (setup.ProviderSetupItem, error) {
	setupID := setupProviderIDForVoiceProvider(providerID)
	if setupID == "" {
		return setup.ProviderSetupItem{}, nil
	}
	return d.setupProvider(ctx, setupID)
}

func (d *Dispatcher) voiceProviderFormResult(ctx context.Context, moduleID string, providerID string, provider setup.ProviderSetupItem, data setup.ProviderFormState, message string) (Result, error) {
	if strings.TrimSpace(provider.ID) == "" {
		return d.voiceModuleProviderPicker(ctx, moduleID)
	}
	capabilities := providerFormCapabilities(provider)
	result := customProviderFormResult(customProviderFormResultData{
		Title:                  voiceProviderFormTitle(moduleID, providerID, provider),
		Data:                   data.WithDefaultProviderOptions(capabilities),
		ProviderID:             provider.ID,
		CatalogID:              providerFormCatalogID(provider),
		ProviderType:           providerFormType(provider),
		KeyStatus:              editSecretFieldStatus(provider, data.APIKey),
		IncludeIdentity:        false,
		IncludeReasoningEffort: false,
		IncludeToolProfile:     false,
		SubmitCommand: func(token string) string {
			return voiceModuleCommand(moduleID, "provider-save", providerID, token)
		},
		CancelCommand: voiceModuleCommand(moduleID, "provider"),
		EditCommand: func(field string, token string) string {
			return voiceModuleCommand(moduleID, "provider-field", field, providerID, token)
		},
		Error: message,
	})
	if result.Form != nil {
		result.Form.Fields = voiceProviderFormFields(result.Form.Fields)
	}
	return result, nil
}

func voiceProviderFormFields(fields []FormField) []FormField {
	out := make([]FormField, 0, len(fields))
	for _, field := range fields {
		switch field.ID {
		case "key":
			out = append(out, field)
		}
	}
	return out
}

func setupProviderIDForVoiceProvider(providerID string) string {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "grok":
		return "xai"
	default:
		return ""
	}
}

func voiceProviderFormTitle(moduleID string, providerID string, provider setup.ProviderSetupItem) string {
	name := firstNonEmptyTrimmed(provider.Name, provider.ID, providerID)
	switch moduleID {
	case setup.VoiceModuleTTS:
		return name + " Text to Speech"
	case setup.VoiceModuleSTT:
		return name + " Speech to Text"
	default:
		return name
	}
}
