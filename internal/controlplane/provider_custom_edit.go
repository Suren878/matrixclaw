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
	providerID := encodeCustomProviderField(provider.ID)
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
					"/provider edit set base "+providerID+" "+token+" ",
					"",
				), nil
			}
		case "model":
			if providerEditModelUsesPicker(provider, data) {
				return d.providerModelPicker(ctx, provider, data), nil
			}
		case "tools":
			token := encodeCustomProviderFormToken(data)
			return customProviderToolModePicker(title, data, "/provider edit set tools "+providerID+" "+token+" ", ""), nil
		case "reasoning":
			token := encodeCustomProviderFormToken(data)
			return customProviderReasoningEffortPickerWithOptions(
				title,
				data,
				providers.ReasoningEffortsForProvider(providerFormCatalogID(provider), providerFormType(provider)),
				"/provider edit set reasoning "+providerID+" "+token+" ",
				"",
			), nil
		}
		token := encodeCustomProviderFormToken(data)
		placeholder := ""
		if field == "key" {
			placeholder = "leave empty to keep"
		}
		return customProviderFieldPrompt(title, field, data, placeholder, "/provider edit set "+field+" "+providerID+" "+token+" ", ""), nil
	case "set":
		data = data.withField(field, valueAfterFields(form, 3))
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

func (d *Dispatcher) providerEditData(ctx context.Context, raw string) (setup.ProviderSetupItem, customProviderForm, string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 2 {
		return setup.ProviderSetupItem{}, customProviderForm{}, "", fmt.Errorf("provider id and form token are required")
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
		return setup.ProviderSetupItem{}, customProviderForm{}, "", err
	}
	data, err := decodeCustomProviderFormToken(fields[tokenIndex])
	if err != nil {
		return setup.ProviderSetupItem{}, customProviderForm{}, "", err
	}
	return provider, data, field, nil
}

func (d *Dispatcher) providerModelPicker(ctx context.Context, provider setup.ProviderSetupItem, data customProviderForm) Result {
	providerID := encodeCustomProviderField(provider.ID)
	token := encodeCustomProviderFormToken(data)
	models, err := d.providers.ProviderModels(ctx, provider.ID, providerUpdateFromForm(provider, data, false))
	if err != nil {
		return customProviderFieldPrompt(
			"Edit "+firstNonEmptyTrimmed(provider.Name, provider.ID),
			"model",
			data,
			"Could not load remote models: "+err.Error()+". Enter the model manually.",
			"/provider edit set model "+providerID+" "+token+" ",
			"",
		)
	}
	current := strings.TrimSpace(data.Model)
	items := make([]PickerItem, 0, len(models)+1)
	for _, modelID := range models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		items = append(items, PickerItem{
			ID:       modelID,
			Title:    modelID,
			Selected: modelID == current,
			Command:  "/provider edit set model " + providerID + " " + token + " " + modelID,
		})
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, "Model").
			Back("").
			Items(items...).
			Ptr(),
	}
}

func providerEditBaseURLUsesPicker(provider setup.ProviderSetupItem, data customProviderForm) bool {
	spec := setup.ProviderFormSpecFromInput(setup.ProviderFormSpecInput{
		ID:                provider.ID,
		CatalogID:         providerFormCatalogID(provider),
		Name:              data.Name,
		Type:              providerFormType(provider),
		BaseURL:           data.BaseURL,
		Model:             data.Model,
		ReasoningEffort:   data.Reasoning,
		ToolUseMode:       data.ToolUseMode,
		Custom:            isCustomSetupProvider(provider),
		CustomKnown:       true,
		Capabilities:      providerFormCapabilities(provider),
		CapabilitiesKnown: true,
	})
	field, ok := spec.Field(setup.ProviderFormFieldBaseURL)
	return ok && field.Picker
}

func providerEditModelUsesPicker(provider setup.ProviderSetupItem, data customProviderForm) bool {
	spec := setup.ProviderFormSpecForSetupItem(provider)
	if isCustomSetupProvider(provider) {
		spec = setup.ProviderFormSpecFromInput(setup.ProviderFormSpecInput{
			ID:                provider.ID,
			CatalogID:         provider.CatalogID,
			Name:              data.Name,
			Type:              providerFormType(provider),
			BaseURL:           data.BaseURL,
			Model:             data.Model,
			ReasoningEffort:   data.Reasoning,
			ToolUseMode:       data.ToolUseMode,
			Custom:            true,
			CustomKnown:       true,
			Capabilities:      providerFormCapabilities(provider),
			CapabilitiesKnown: true,
		})
	}
	field, ok := spec.Field(setup.ProviderFormFieldModel)
	return ok && field.Picker
}

func providerEditFormResult(provider setup.ProviderSetupItem, data customProviderForm, message string) Result {
	providerID := encodeCustomProviderField(provider.ID)
	includeIdentity := isCustomSetupProvider(provider)
	capabilities := providerFormCapabilities(provider)
	return customProviderFormResult(customProviderFormResultData{
		Title:                  "Edit " + firstNonEmptyTrimmed(provider.Name, provider.ID),
		Data:                   data.withDefaultProviderOptions(capabilities),
		ProviderID:             provider.ID,
		CatalogID:              providerFormCatalogID(provider),
		ProviderType:           providerFormType(provider),
		KeyStatus:              editSecretFieldStatus(provider, data.APIKey),
		IncludeIdentity:        includeIdentity,
		IncludeReasoningEffort: capabilities.ReasoningEffort,
		IncludeToolProfile:     capabilities.ToolCalling,
		SubmitCommand: func(token string) string {
			return "/provider edit save " + providerID + " " + token
		},
		CancelCommand: "",
		EditCommand: func(field string, token string) string {
			return "/provider edit field " + field + " " + providerID + " " + token
		},
		Error: message,
	})
}

func formFromProvider(provider setup.ProviderSetupItem) customProviderForm {
	catalogID := providerFormCatalogID(provider)
	providerType := providerFormType(provider)
	defaultModel := strings.TrimSpace(provider.DefaultModel)
	if defaultModel == "" {
		if entry, ok := providers.CatalogEntryByID(catalogID); ok {
			defaultModel = strings.TrimSpace(entry.DefaultModel)
		}
	}
	return customProviderForm{
		Name:        firstNonEmptyTrimmed(provider.Name, provider.ID),
		BaseURL:     strings.TrimSpace(provider.BaseURL),
		Model:       strings.TrimSpace(firstNonEmptyTrimmed(provider.Model, defaultModel)),
		Reasoning:   firstNonEmptyTrimmed(provider.ReasoningEffort, providers.DefaultReasoningEffortForProvider(catalogID, providerType)),
		ToolUseMode: provider.ToolUseMode,
	}.withDefaultProviderOptions(providerFormCapabilities(provider))
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
	switch strings.TrimSpace(value) {
	case "name", "base", "model", "key", "reasoning", "tools":
		return true
	default:
		return false
	}
}

func providerEditValidationMessage(provider setup.ProviderSetupItem, data customProviderForm) string {
	requireIdentity := isCustomSetupProvider(provider)
	if requireIdentity {
		if strings.TrimSpace(data.Name) == "" {
			return "Provider name is required."
		}
		if strings.TrimSpace(data.BaseURL) == "" {
			return "Base URL is required."
		}
	}
	if strings.TrimSpace(data.Model) == "" {
		return "Model is required."
	}
	if !provider.Configured && strings.TrimSpace(data.APIKey) == "" && strings.TrimSpace(provider.APIKeyPreview) == "" {
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
	if entry, ok := providers.CatalogEntryByID(providerFormCatalogID(provider)); ok {
		return entry.Type
	}
	return ""
}

func providerFormCapabilities(provider setup.ProviderSetupItem) providers.Capabilities {
	if provider.Capabilities.ModelDiscovery || provider.Capabilities.ReasoningEffort || provider.Capabilities.ToolCalling || provider.Capabilities.NormalizeModel {
		return provider.Capabilities
	}
	return providers.ProviderCapabilities(providerFormCatalogID(provider), providerFormType(provider))
}

func (d *Dispatcher) saveProviderEdit(ctx context.Context, session *core.Session, provider setup.ProviderSetupItem, data customProviderForm) (Result, error) {
	data = data.withDefaultProviderOptions(providerFormCapabilities(provider))
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
	result := providerEditFormResult(configured, formFromProvider(configured), "Saved.")
	result.ReloadSnapshot = true
	return result, nil
}

func providerUpdateFromForm(provider setup.ProviderSetupItem, data customProviderForm, active bool) setup.ProviderSetupUpdate {
	return setup.ProviderSetupUpdate{
		Name:            data.Name,
		Type:            providerFormType(provider),
		BaseURL:         data.BaseURL,
		Model:           data.Model,
		APIKey:          data.APIKey,
		ReasoningEffort: data.Reasoning,
		ToolUseMode:     data.ToolUseMode,
		Active:          active,
	}
}
