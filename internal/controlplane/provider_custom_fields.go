package controlplane

import (
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func encodeCustomProviderField(value string) string {
	return url.QueryEscape(strings.TrimSpace(value))
}

func decodeCustomProviderField(value string) (string, error) {
	decoded, err := url.QueryUnescape(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(decoded), nil
}

func encodeCustomProviderFormToken(data setup.ProviderFormState) string {
	fields := []string{
		data.Name,
		data.BaseURL,
		data.Model,
		data.APIKey,
		data.ReasoningEffort,
		string(data.ToolUseMode),
	}
	return encodeCustomProviderField(strings.Join(fields, "\x1f"))
}

func decodeCustomProviderFormToken(token string) (setup.ProviderFormState, error) {
	decoded, err := decodeCustomProviderField(token)
	if err != nil {
		return setup.ProviderFormState{}, err
	}
	parts := strings.Split(decoded, "\x1f")
	if len(parts) == 5 {
		parts = append(parts[:4], "", parts[4])
	}
	for len(parts) < 6 {
		parts = append(parts, "")
	}
	return setup.ProviderFormState{
		Name:            strings.TrimSpace(parts[0]),
		BaseURL:         strings.TrimSpace(parts[1]),
		Model:           strings.TrimSpace(parts[2]),
		APIKey:          strings.TrimSpace(parts[3]),
		ReasoningEffort: strings.TrimSpace(parts[4]),
		ToolUseMode:     providers.ToolUseMode(strings.TrimSpace(parts[5])),
	}, nil
}

func controlplaneCommand(parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return ""
	}
	return "/" + strings.Join(values, " ")
}

func providerCommand(parts ...string) string {
	values := append([]string{"provider"}, parts...)
	return controlplaneCommand(values...)
}

func providerCommandPrefix(parts ...string) string {
	return providerCommand(parts...) + " "
}

func providerEncodedID(providerID string) string {
	return encodeCustomProviderField(providerID)
}

func providerUseCommand(providerID string) string {
	return providerCommand("use", providerEncodedID(providerID))
}

func providerEditCommand(providerID string) string {
	return providerCommand("edit", providerEncodedID(providerID))
}

func providerEditFieldCommand(field string, providerID string, token string) string {
	return providerCommand("edit", "field", field, providerEncodedID(providerID), token)
}

func providerEditSetCommand(field string, providerID string, token string, value string) string {
	return providerCommand("edit", "set", field, providerEncodedID(providerID), token, value)
}

func providerEditSetCommandPrefix(field string, providerID string, token string) string {
	return providerCommandPrefix("edit", "set", field, providerEncodedID(providerID), token)
}

func providerEditSaveCommand(providerID string, token string) string {
	return providerCommand("edit", "save", providerEncodedID(providerID), token)
}

func providerKeyCommandPrefix(providerID string) string {
	return providerCommandPrefix("key", providerEncodedID(providerID))
}

func customProviderCommand(parts ...string) string {
	values := append([]string{"provider", "custom"}, parts...)
	return controlplaneCommand(values...)
}

func customProviderCommandPrefix(parts ...string) string {
	return customProviderCommand(parts...) + " "
}

type customProviderFormResultData struct {
	Title                  string
	Data                   setup.ProviderFormState
	ProviderID             string
	CatalogID              string
	ProviderType           string
	KeyStatus              string
	IncludeIdentity        bool
	IncludeReasoningEffort bool
	IncludeToolProfile     bool
	SubmitCommand          func(token string) string
	CancelCommand          string
	EditCommand            func(field string, token string) string
	Error                  string
}

func customProviderFormResult(config customProviderFormResultData) Result {
	token := encodeCustomProviderFormToken(config.Data)
	fields := customProviderFormFieldsForProvider(config.Data, config.KeyStatus, config.ProviderID, config.CatalogID, config.ProviderType, config.IncludeIdentity, config.IncludeReasoningEffort, config.IncludeToolProfile, func(field string) string {
		return config.EditCommand(field, token)
	})
	return Result{
		Handled: true,
		Form: &FormData{
			Title:         config.Title,
			Fields:        fields,
			SubmitLabel:   "Save",
			CancelLabel:   "Cancel",
			SubmitCommand: config.SubmitCommand(token),
			CancelCommand: config.CancelCommand,
			Error:         strings.TrimSpace(config.Error),
		},
	}
}

func customProviderFormFieldsForProvider(data setup.ProviderFormState, keyStatus string, providerID string, catalogID string, providerType string, includeIdentity bool, includeReasoningEffort bool, includeToolProfile bool, editCommand func(string) string) []FormField {
	if strings.TrimSpace(providerType) == "" {
		capabilities := providers.Capabilities{
			ReasoningEffort: includeReasoningEffort,
			ToolCalling:     includeToolProfile,
		}
		return customProviderFormFieldsFromSpec(data, keyStatus, editCommand, setup.ProviderFormSpecFromInput(setup.ProviderFormSpecInput{
			ID:                providerID,
			CatalogID:         catalogID,
			Name:              data.Name,
			Type:              providerType,
			BaseURL:           data.BaseURL,
			Model:             data.Model,
			APIKey:            data.APIKey,
			ReasoningEffort:   data.ReasoningEffort,
			ToolUseMode:       data.ToolUseMode,
			Custom:            includeIdentity,
			CustomKnown:       true,
			Capabilities:      capabilities,
			CapabilitiesKnown: true,
		}))
	}
	spec := setup.ProviderFormSpecFromInput(setup.ProviderFormSpecInput{
		ID:                providerID,
		CatalogID:         catalogID,
		Name:              data.Name,
		Type:              providerType,
		BaseURL:           data.BaseURL,
		Model:             data.Model,
		APIKey:            data.APIKey,
		ReasoningEffort:   data.ReasoningEffort,
		ToolUseMode:       data.ToolUseMode,
		Custom:            includeIdentity,
		CustomKnown:       true,
		Capabilities:      providers.Capabilities{ReasoningEffort: includeReasoningEffort, ToolCalling: includeToolProfile},
		CapabilitiesKnown: true,
	})
	return customProviderFormFieldsFromSpec(data, keyStatus, editCommand, spec)
}

func customProviderFormFieldsFromSpec(data setup.ProviderFormState, keyStatus string, editCommand func(string) string, spec setup.ProviderFormSpec) []FormField {
	viewFields := setup.ProviderFormViewFields(spec, setup.ProviderFormViewOptions{
		APIKeyStatus:       keyStatus,
		ShowRequiredStatus: true,
	})
	fields := make([]FormField, 0, len(viewFields)+1)
	if providers.PolicyForProvider(firstNonEmptyTrimmed(spec.CatalogID, spec.ID), spec.Type).AuthMode == providers.ProviderAuthOAuth {
		fields = append(fields, FormField{
			ID:          "auth",
			Label:       "Authorization",
			Value:       openAICodexAuthInfo(),
			EditCommand: providerCommand("auth", providerEncodedID(spec.ID)),
		})
	}
	for _, field := range viewFields {
		editCommandValue := ""
		if !field.Disabled {
			editCommandValue = editCommand(field.CommandID)
		}
		fields = append(fields, FormField{
			ID:          field.CommandID,
			Label:       field.Label,
			Value:       field.Status,
			EditCommand: editCommandValue,
			Disabled:    field.Disabled,
		})
	}
	return fields
}

func customProviderToolModePicker(titlePrefix string, data setup.ProviderFormState, submitPrefix string, cancelCommand string) Result {
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, titlePrefix+": tool mode").
			HideBack(true).
			Close(cancelCommand).
			Items(customProviderChoiceItems(providerToolUseField(data), submitPrefix)...).
			Ptr(),
	}
}

func providerToolUseField(data setup.ProviderFormState) setup.ProviderFormField {
	spec := setup.ProviderFormSpecFromInput(setup.ProviderFormSpecInput{
		Type:              providers.TypeOpenAICompat,
		Model:             data.Model,
		ToolUseMode:       data.ToolUseMode,
		Custom:            true,
		CustomKnown:       true,
		Capabilities:      providers.Capabilities{ToolCalling: true},
		CapabilitiesKnown: true,
	})
	field, _ := spec.Field(setup.ProviderFormFieldToolUse)
	return field
}

func customProviderBaseURLPicker(titlePrefix string, data setup.ProviderFormState, catalogID string, submitPrefix string, cancelCommand string) Result {
	field, ok := setup.ProviderFormSpecFromInput(setup.ProviderFormSpecInput{
		CatalogID: catalogID,
		BaseURL:   data.BaseURL,
		Custom:    false,
	}).Field(setup.ProviderFormFieldBaseURL)
	if !ok || len(field.Choices) == 0 {
		return customProviderFieldPrompt(titlePrefix, "base", data, "", submitPrefix, cancelCommand)
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, titlePrefix+": endpoint").
			Back(cancelCommand).
			Items(customProviderChoiceItems(field, submitPrefix)...).
			Ptr(),
	}
}

func customProviderReasoningEffortPickerWithOptions(titlePrefix string, data setup.ProviderFormState, efforts []string, submitPrefix string, cancelCommand string) Result {
	field := providerReasoningField(data, efforts)
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, titlePrefix+": reasoning effort").
			HideBack(true).
			Close(cancelCommand).
			Items(customProviderChoiceItems(field, submitPrefix)...).
			Ptr(),
	}
}

func providerReasoningField(data setup.ProviderFormState, efforts []string) setup.ProviderFormField {
	spec := setup.ProviderFormSpecFromInput(setup.ProviderFormSpecInput{
		Type:              providers.TypeOpenAICompat,
		Model:             data.Model,
		ReasoningEffort:   data.ReasoningEffort,
		Custom:            true,
		CustomKnown:       true,
		Capabilities:      providers.Capabilities{ReasoningEffort: true},
		CapabilitiesKnown: true,
	})
	field, _ := spec.Field(setup.ProviderFormFieldReasoningEffort)
	if len(efforts) > 0 {
		field.Choices = make([]setup.ProviderFormChoice, 0, len(efforts))
		current := providers.NormalizeReasoningEffort(data.ReasoningEffort)
		if current == "" {
			current = providers.DefaultReasoningEffort
		}
		for _, effort := range efforts {
			effort = providers.NormalizeReasoningEffort(effort)
			if effort == "" {
				continue
			}
			field.Choices = append(field.Choices, setup.ProviderFormChoice{
				ID:       effort,
				Title:    strings.Title(effort),
				Value:    effort,
				Selected: current == effort,
			})
		}
	}
	return field
}

func customProviderChoiceItems(field setup.ProviderFormField, prefix string) []PickerItem {
	items := make([]PickerItem, 0, len(field.Choices))
	for _, choice := range field.Choices {
		value := strings.TrimSpace(choice.Value)
		if value == "" {
			value = strings.TrimSpace(choice.ID)
		}
		items = append(items, PickerItem{
			ID:       firstNonEmptyTrimmed(choice.ID, value),
			Title:    firstNonEmptyTrimmed(choice.Title, value),
			Info:     strings.TrimSpace(choice.Status),
			Selected: choice.Selected,
			Focused:  field.ID == setup.ProviderFormFieldToolUse && choice.Selected,
			Command:  prefix + value,
		})
		if field.ID == setup.ProviderFormFieldToolUse {
			items[len(items)-1].Selected = false
		}
	}
	return items
}

func firstField(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func valueAfterFields(value string, count int) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) < count {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), strings.Join(fields[:count], " ")))
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func customProviderID(name string) string {
	normalized := providers.NormalizeProviderID(name)
	var out []rune
	lastDash := false
	for _, r := range normalized {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r)
			lastDash = false
		case r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		default:
			if !lastDash {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	return strings.Trim(string(out), "-")
}

func customProviderStepField(step string, prefix string) (string, bool) {
	field, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(step)), prefix)
	return field, ok && isCustomProviderFormField(field)
}

func customProviderFieldPrompt(titlePrefix string, field string, data setup.ProviderFormState, placeholder string, submitPrefix string, cancelCommand string) Result {
	if placeholder == "" {
		placeholder = customProviderFieldPlaceholder(field)
	}
	return Result{
		Handled: true,
		Prompt: &PromptData{
			Title:               titlePrefix + ": " + customProviderFieldTitle(field),
			Placeholder:         placeholder,
			Value:               data.Field(field),
			SubmitCommandPrefix: submitPrefix,
			CancelCommand:       cancelCommand,
			Sensitive:           field == "key",
		},
	}
}
