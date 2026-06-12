package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleProviderEdit(ctx context.Context, session *core.Session, args string) (Result, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return Result{Handled: true, Text: "Provider id is required."}, nil
	}
	step := strings.ToLower(strings.TrimSpace(fields[0]))
	switch step {
	case "field", "set", "save":
		return d.handleProviderEditStep(ctx, session, step, strings.TrimSpace(strings.TrimPrefix(args, fields[0])))
	default:
		provider, err := d.setupProvider(ctx, fields[0])
		if err != nil {
			return Result{}, err
		}
		return providerEditFormResult(provider, formFromProvider(provider), ""), nil
	}
}

func (d *Dispatcher) handleProviderEditStep(ctx context.Context, session *core.Session, step string, form string) (Result, error) {
	provider, data, field, err := d.providerEditData(ctx, form)
	if err != nil {
		return Result{}, err
	}
	title := "Edit " + firstNonEmptyTrimmed(provider.Name, provider.ID)
	switch step {
	case "field":
		switch strings.TrimSpace(field) {
		case "base":
			if providerEditBaseURLUsesPicker(provider, data) {
				token := encodeCustomProviderFormToken(data)
				return customProviderBaseURLPicker(
					title,
					data,
					providerFormCatalogID(provider),
					providerEditSetCommandPrefix("base", provider.ID, token),
					providerEditSetCommand("base", provider.ID, token, data.BaseURL),
				), nil
			}
		case "model":
			if message := providerEditModelDisabledMessage(provider, data); message != "" {
				return providerEditFormResult(provider, data, message), nil
			}
			if providerEditModelUsesPicker(provider, data) {
				return d.providerModelPicker(ctx, provider, data), nil
			}
		case "tools":
			token := encodeCustomProviderFormToken(data)
			return customProviderToolModePicker(
				title,
				data,
				providerEditSetCommandPrefix("tools", provider.ID, token),
				providerEditSetCommand("tools", provider.ID, token, string(data.ToolUseMode)),
			), nil
		case "reasoning":
			token := encodeCustomProviderFormToken(data)
			return customProviderReasoningEffortPickerWithOptions(
				title,
				data,
				providers.ReasoningEffortsForProvider(providerFormCatalogID(provider), providerFormType(provider)),
				providerEditSetCommandPrefix("reasoning", provider.ID, token),
				providerEditSetCommand("reasoning", provider.ID, token, data.ReasoningEffort),
			), nil
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
			providerEditSetCommandPrefix(field, provider.ID, token),
			providerEditSetCommand(field, provider.ID, token, data.Field(field)),
		), nil
	case "set":
		data = data.WithField(field, valueAfterFields(form, 3))
		return providerEditFormResult(provider, data, ""), nil
	case "save":
		if message := providerEditValidationMessage(provider, data); message != "" {
			return providerEditFormResult(provider, data, message), nil
		}
		return d.saveProviderEdit(ctx, session, provider, data)
	default:
		return Result{Handled: true, Text: "Unknown provider edit step."}, nil
	}
}

func (d *Dispatcher) providerEditData(ctx context.Context, raw string) (setup.ProviderSetupItem, setup.ProviderFormState, string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 2 {
		return setup.ProviderSetupItem{}, setup.ProviderFormState{}, "", fmt.Errorf("provider id and form token are required")
	}
	field := ""
	providerField := fields[0]
	tokenIndex := 1
	if len(fields) >= 3 && isCustomProviderFormField(providerField) {
		field = providerField
		providerField = fields[1]
		tokenIndex = 2
	}
	provider, err := d.setupProvider(ctx, providerField)
	if err != nil {
		return setup.ProviderSetupItem{}, setup.ProviderFormState{}, "", err
	}
	data, err := decodeCustomProviderFormToken(fields[tokenIndex])
	if err != nil {
		return setup.ProviderSetupItem{}, setup.ProviderFormState{}, "", err
	}
	return provider, data, field, nil
}

func (d *Dispatcher) providerModelPicker(ctx context.Context, provider setup.ProviderSetupItem, data setup.ProviderFormState) Result {
	token := encodeCustomProviderFormToken(data)
	response, err := d.providers.ProviderModelCatalog(ctx, provider.ID, providerUpdateFromForm(provider, data, false))
	if err != nil {
		return providerEditFormResult(provider, data, "Could not load remote models: "+err.Error())
	}
	if response.Status != setup.ProviderModelStatusOK {
		message := setup.ProviderModelCatalogMessage(response)
		if setup.ProviderModelCatalogAllowsManualInput(response) {
			return providerEditManualModelPrompt(provider, data, token, setup.ProviderModelCatalogManualMessage(response))
		}
		return providerEditFormResult(provider, data, message)
	}
	current := strings.TrimSpace(data.Model)
	items := make([]PickerItem, 0, len(response.Models)+1)
	for _, modelID := range response.Models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		items = append(items, PickerItem{
			ID:       modelID,
			Title:    modelID,
			Selected: modelID == current,
			Command:  providerEditSetCommand("model", provider.ID, token, modelID),
		})
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, "Model").
			Select(providerEditSetCommand("model", provider.ID, token, data.Model)).
			Items(items...).
			Ptr(),
	}
}

func providerEditManualModelPrompt(provider setup.ProviderSetupItem, data setup.ProviderFormState, token string, message string) Result {
	return customProviderFieldPrompt(
		"Edit "+firstNonEmptyTrimmed(provider.Name, provider.ID),
		"model",
		data,
		message,
		providerEditSetCommandPrefix("model", provider.ID, token),
		providerEditSetCommand("model", provider.ID, token, data.Model),
	)
}

func providerEditBaseURLUsesPicker(provider setup.ProviderSetupItem, data setup.ProviderFormState) bool {
	spec := providerEditFormSpec(provider, data)
	field, ok := spec.Field(setup.ProviderFormFieldBaseURL)
	return ok && field.Picker
}

func providerEditModelUsesPicker(provider setup.ProviderSetupItem, data setup.ProviderFormState) bool {
	spec := providerEditFormSpec(provider, data)
	field, ok := spec.Field(setup.ProviderFormFieldModel)
	return ok && field.Picker && !field.Disabled
}

func providerEditModelDisabledMessage(provider setup.ProviderSetupItem, data setup.ProviderFormState) string {
	spec := providerEditFormSpec(provider, data)
	field, ok := spec.Field(setup.ProviderFormFieldModel)
	if ok && field.Disabled {
		return "Enter an API key before loading models."
	}
	return ""
}

func providerEditFormSpec(provider setup.ProviderSetupItem, data setup.ProviderFormState) setup.ProviderFormSpec {
	return setup.ProviderFormSpecFromInput(setup.ProviderFormSpecInput{
		ID:                  provider.ID,
		CatalogID:           providerFormCatalogID(provider),
		Name:                data.Name,
		Type:                providerFormType(provider),
		BaseURL:             data.BaseURL,
		BaseURLOptions:      provider.BaseURLOptions,
		Model:               data.Model,
		ReasoningEffort:     data.ReasoningEffort,
		ToolUseMode:         data.ToolUseMode,
		HasStoredAPIKey:     strings.TrimSpace(provider.APIKeyPreview) != "",
		StoredAPIKeyPreview: provider.APIKeyPreview,
		Custom:              isCustomSetupProvider(provider),
		CustomKnown:         true,
		Capabilities:        providerFormCapabilities(provider),
		CapabilitiesKnown:   true,
	})
}

func providerEditFormResult(provider setup.ProviderSetupItem, data setup.ProviderFormState, message string) Result {
	includeIdentity := isCustomSetupProvider(provider)
	capabilities := providerFormCapabilities(provider)
	return customProviderFormResult(customProviderFormResultData{
		Title:                  "Edit " + firstNonEmptyTrimmed(provider.Name, provider.ID),
		Data:                   data.WithDefaultProviderOptions(capabilities),
		ProviderID:             provider.ID,
		CatalogID:              providerFormCatalogID(provider),
		ProviderType:           providerFormType(provider),
		KeyStatus:              editSecretFieldStatus(provider, data.APIKey),
		IncludeIdentity:        includeIdentity,
		IncludeReasoningEffort: capabilities.ReasoningEffort,
		IncludeToolProfile:     capabilities.ToolCalling,
		SubmitCommand: func(token string) string {
			return providerEditSaveCommand(provider.ID, token)
		},
		CancelCommand: providerCommand(),
		EditCommand: func(field string, token string) string {
			return providerEditFieldCommand(field, provider.ID, token)
		},
		Error: message,
	})
}

func formFromProvider(provider setup.ProviderSetupItem) setup.ProviderFormState {
	catalogID := providerFormCatalogID(provider)
	providerType := providerFormType(provider)
	defaultModel := strings.TrimSpace(provider.DefaultModel)
	if defaultModel == "" {
		defaultModel = strings.TrimSpace(providers.PolicyForProvider(catalogID, providerType).DefaultModel)
	}
	return setup.ProviderFormState{
		Name:            firstNonEmptyTrimmed(provider.Name, provider.ID),
		BaseURL:         strings.TrimSpace(provider.BaseURL),
		Model:           strings.TrimSpace(firstNonEmptyTrimmed(provider.Model, defaultModel)),
		ReasoningEffort: firstNonEmptyTrimmed(provider.ReasoningEffort, providers.DefaultReasoningEffortForModel(catalogID, providerType, firstNonEmptyTrimmed(provider.Model, defaultModel))),
		ToolUseMode:     provider.ToolUseMode,
	}.WithDefaultProviderOptions(providerFormCapabilities(provider))
}

func editSecretFieldStatus(provider setup.ProviderSetupItem, value string) string {
	if value := strings.TrimSpace(value); value != "" {
		return setup.MaskSecret(value)
	}
	if preview := strings.TrimSpace(provider.APIKeyPreview); preview != "" {
		return preview
	}
	return "Required"
}

func isCustomProviderFormField(value string) bool {
	_, ok := setup.ProviderFormFieldIDFromCommand(value)
	return ok
}

func providerEditValidationMessage(provider setup.ProviderSetupItem, data setup.ProviderFormState) string {
	requireIdentity := isCustomSetupProvider(provider)
	if requireIdentity {
		if strings.TrimSpace(data.Name) == "" {
			return "Provider name is required."
		}
	}
	if field, ok := providerEditFormSpec(provider, data).Field(setup.ProviderFormFieldBaseURL); ok && field.Required && strings.TrimSpace(data.BaseURL) == "" {
		return "Base URL is required."
	}
	if strings.TrimSpace(data.Model) == "" {
		return "Model is required."
	}
	if providerPolicy(provider).RequiresAPIKey && !provider.Configured && strings.TrimSpace(data.APIKey) == "" && strings.TrimSpace(provider.APIKeyPreview) == "" {
		return "API key is required."
	}
	return ""
}

func providerFormCatalogID(provider setup.ProviderSetupItem) string {
	if catalogID := providers.NormalizeProviderID(provider.CatalogID); catalogID != "" {
		return catalogID
	}
	return providers.NormalizeProviderID(provider.ID)
}

func providerFormType(provider setup.ProviderSetupItem) string {
	if providerType := strings.TrimSpace(provider.Type); providerType != "" {
		return providerType
	}
	return providers.PolicyForProvider(providerFormCatalogID(provider), "").Type
}

func providerFormCapabilities(provider setup.ProviderSetupItem) providers.Capabilities {
	if provider.Capabilities.ModelDiscovery || provider.Capabilities.ReasoningEffort || provider.Capabilities.ToolCalling || provider.Capabilities.NormalizeModel {
		return provider.Capabilities
	}
	return providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
		ProviderID:   providerFormCatalogID(provider),
		ProviderType: providerFormType(provider),
		ModelID:      firstNonEmptyTrimmed(provider.Model, provider.DefaultModel),
	}).ProviderCapabilities
}

func (d *Dispatcher) saveProviderEdit(ctx context.Context, session *core.Session, provider setup.ProviderSetupItem, data setup.ProviderFormState) (Result, error) {
	data = data.WithDefaultProviderOptions(providerFormCapabilities(provider))
	configured, err := d.providers.ConfigureSetupProvider(ctx, provider.ID, providerUpdateFromForm(provider, data, !provider.Configured))
	if err != nil {
		return Result{}, err
	}
	if session != nil {
		updated, updateErr := d.providers.UpdateSessionProvider(ctx, session.ID, configured.ID)
		if updateErr == nil {
			configured.Active = true
			configured.Model = updated.ModelID
		}
	}
	providers, err := d.providers.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled:        true,
		Picker:         NewPickerData(PickerProvider, "Provider").Items(ProviderPickerItems(providers, session)...).Ptr(),
		ReloadSnapshot: true,
	}, nil
}

func providerUpdateFromForm(provider setup.ProviderSetupItem, data setup.ProviderFormState, active bool) setup.ProviderSetupUpdate {
	return data.ToSetupUpdate(providerFormType(provider), active)
}
