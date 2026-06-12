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
		if message := providerEditModelDisabledMessage(provider, data); message != "" {
			return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, data, message)
		}
		if providerEditModelUsesPicker(provider, data) {
			return d.voiceProviderModelPicker(ctx, moduleID, providerID, provider, data)
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

func (d *Dispatcher) voiceModuleProviderSetupFormFromToken(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, rest)
	if err != nil {
		return Result{}, err
	}
	return d.voiceProviderSetupFormResult(ctx, moduleID, providerID, provider, data, "")
}

func (d *Dispatcher) voiceModuleProviderSetupField(ctx context.Context, moduleID string, args string) (Result, error) {
	field, rest := firstCommandStep(args)
	providerID, rest := firstCommandToken(rest)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, rest)
	if err != nil {
		return Result{}, err
	}
	token := encodeCustomProviderFormToken(data)
	return customProviderFieldPrompt(
		voiceProviderFormTitle(moduleID, providerID, provider),
		field,
		data,
		"leave empty to keep",
		voiceModuleCommandPrefix(moduleID, "provider-setup-set", field, providerID, token),
		voiceModuleCommand(moduleID, "provider-setup-form", providerID, token),
	), nil
}

func (d *Dispatcher) voiceModuleProviderSetupSet(ctx context.Context, moduleID string, args string) (Result, error) {
	field, rest := firstCommandStep(args)
	providerID, rest := firstCommandToken(rest)
	token := firstField(rest)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, token)
	if err != nil {
		return Result{}, err
	}
	data = data.WithField(field, strings.TrimSpace(strings.TrimPrefix(rest, token)))
	return d.voiceProviderSetupFormResult(ctx, moduleID, providerID, provider, data, "")
}

func (d *Dispatcher) saveVoiceModuleProviderSetup(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, rest)
	if err != nil {
		return Result{}, err
	}
	if message := providerEditValidationMessage(provider, data); message != "" {
		return d.voiceProviderSetupFormResult(ctx, moduleID, providerID, provider, data, message)
	}
	if _, err := d.providers.ConfigureSetupProvider(ctx, provider.ID, providerUpdateFromForm(provider, data, false)); err != nil {
		return Result{}, err
	}
	return d.voiceModuleProviderSetup(ctx, moduleID, "")
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

func (d *Dispatcher) voiceProviderModelPicker(ctx context.Context, moduleID string, providerID string, provider setup.ProviderSetupItem, data setup.ProviderFormState) (Result, error) {
	token := encodeCustomProviderFormToken(data)
	response, err := d.providers.ProviderModelCatalog(ctx, provider.ID, providerUpdateFromForm(provider, data, false))
	if err != nil {
		return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, data, "Could not load remote models: "+err.Error())
	}
	if response.Status != setup.ProviderModelStatusOK {
		return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, data, setup.ProviderModelCatalogMessage(response))
	}
	current := strings.TrimSpace(data.Model)
	items := make([]PickerItem, 0, len(response.Models))
	for _, modelID := range response.Models {
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
				Select(voiceModuleCommand(moduleID, "provider-form", providerID, token)).
				Items(items...).
				Ptr(),
	}, nil
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
	return d.voiceProviderFormResultWithCommands(ctx, moduleID, providerID, provider, data, message, voiceModuleCommand(moduleID, "provider"), func(token string) string {
		return voiceModuleCommand(moduleID, "provider-save", providerID, token)
	}, func(field string, token string) string {
		return voiceModuleCommand(moduleID, "provider-field", field, providerID, token)
	})
}

func (d *Dispatcher) voiceProviderSetupFormResult(ctx context.Context, moduleID string, providerID string, provider setup.ProviderSetupItem, data setup.ProviderFormState, message string) (Result, error) {
	return d.voiceProviderFormResultWithCommands(ctx, moduleID, providerID, provider, data, message, voiceModuleCommand(moduleID, "provider-setup"), func(token string) string {
		return voiceModuleCommand(moduleID, "provider-setup-save", providerID, token)
	}, func(field string, token string) string {
		return voiceModuleCommand(moduleID, "provider-setup-field", field, providerID, token)
	})
}

func (d *Dispatcher) voiceProviderFormResultWithCommands(ctx context.Context, moduleID string, providerID string, provider setup.ProviderSetupItem, data setup.ProviderFormState, message string, cancelCommand string, submitCommand func(string) string, editCommand func(string, string) string) (Result, error) {
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
		SubmitCommand:          submitCommand,
		CancelCommand:          cancelCommand,
		EditCommand:            editCommand,
		Error:                  message,
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
	return ""
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
