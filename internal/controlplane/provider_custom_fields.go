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

func encodeCustomProviderFormToken(data customProviderForm) string {
	fields := []string{
		data.Name,
		data.BaseURL,
		data.Model,
		data.APIKey,
		data.Reasoning,
		string(data.ToolUseMode),
	}
	return encodeCustomProviderField(strings.Join(fields, "\x1f"))
}

func decodeCustomProviderFormToken(token string) (customProviderForm, error) {
	decoded, err := decodeCustomProviderField(token)
	if err != nil {
		return customProviderForm{}, err
	}
	parts := strings.Split(decoded, "\x1f")
	if len(parts) == 5 {
		parts = append(parts[:4], "", parts[4])
	}
	for len(parts) < 6 {
		parts = append(parts, "")
	}
	return customProviderForm{
		Name:        strings.TrimSpace(parts[0]),
		BaseURL:     strings.TrimSpace(parts[1]),
		Model:       strings.TrimSpace(parts[2]),
		APIKey:      strings.TrimSpace(parts[3]),
		Reasoning:   strings.TrimSpace(parts[4]),
		ToolUseMode: providers.ToolUseMode(strings.TrimSpace(parts[5])),
	}, nil
}

func customProviderCommand(parts ...string) string {
	values := []string{"provider", "custom"}
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return "/" + strings.Join(values, " ")
}

func customProviderCommandPrefix(parts ...string) string {
	return customProviderCommand(parts...) + " "
}

type customProviderFormResultData struct {
	Title                  string
	Data                   customProviderForm
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

func customProviderFormFieldsForProvider(data customProviderForm, keyStatus string, providerID string, catalogID string, providerType string, includeIdentity bool, includeReasoningEffort bool, includeToolProfile bool, editCommand func(string) string) []FormField {
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
			ReasoningEffort:   data.Reasoning,
			ToolUseMode:       data.ToolUseMode,
			Custom:            includeIdentity,
			CustomKnown:       true,
			Capabilities:      capabilities,
			CapabilitiesKnown: true,
		}))
	}
	spec := setup.ProviderFormSpecFromInput(setup.ProviderFormSpecInput{
		ID:              providerID,
		CatalogID:       catalogID,
		Name:            data.Name,
		Type:            providerType,
		BaseURL:         data.BaseURL,
		Model:           data.Model,
		APIKey:          data.APIKey,
		ReasoningEffort: data.Reasoning,
		ToolUseMode:     data.ToolUseMode,
		Custom:          includeIdentity,
		CustomKnown:     true,
	})
	return customProviderFormFieldsFromSpec(data, keyStatus, editCommand, spec)
}

func customProviderFormFieldsFromSpec(data customProviderForm, keyStatus string, editCommand func(string) string, spec setup.ProviderFormSpec) []FormField {
	fields := make([]FormField, 0, len(spec.Fields))
	for _, field := range spec.Fields {
		id := controlplaneProviderFieldID(field.ID)
		value := field.Status
		if field.ID == setup.ProviderFormFieldAPIKey {
			value = keyStatus
		}
		if field.Required {
			value = fieldStatus(value)
		}
		fields = append(fields, FormField{
			ID:          id,
			Label:       controlplaneProviderFieldLabel(field),
			Value:       value,
			EditCommand: editCommand(id),
		})
	}
	return fields
}

func controlplaneProviderFieldID(id setup.ProviderFormFieldID) string {
	switch id {
	case setup.ProviderFormFieldBaseURL:
		return "base"
	case setup.ProviderFormFieldAPIKey:
		return "key"
	case setup.ProviderFormFieldReasoningEffort:
		return "reasoning"
	case setup.ProviderFormFieldToolUse:
		return "tools"
	default:
		return string(id)
	}
}

func controlplaneProviderFieldLabel(field setup.ProviderFormField) string {
	switch field.ID {
	case setup.ProviderFormFieldAPIKey:
		return "API Key"
	case setup.ProviderFormFieldToolUse:
		return "Tool Use"
	case setup.ProviderFormFieldReasoningEffort:
		return "Reasoning Effort"
	default:
		return field.Label
	}
}

func customProviderToolModePicker(titlePrefix string, data customProviderForm, submitPrefix string, cancelCommand string) Result {
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, titlePrefix+": tool mode").
			HideBack(true).
			Close(cancelCommand).
			Items(customProviderToolModeItems(data, submitPrefix)...).
			Ptr(),
	}
}

func customProviderToolModeItems(data customProviderForm, prefix string) []PickerItem {
	current := providers.NormalizeToolUseMode(data.ToolUseMode)
	return []PickerItem{
		{ID: "native", Title: "Enabled", Focused: current == providers.ToolUseNative, Command: prefix + string(providers.ToolUseNative)},
		{ID: "disabled", Title: "Disabled", Focused: current == providers.ToolUseDisabled, Command: prefix + string(providers.ToolUseDisabled)},
	}
}

func customProviderBaseURLPicker(titlePrefix string, data customProviderForm, catalogID string, submitPrefix string, cancelCommand string) Result {
	entry, ok := providers.CatalogEntryByID(catalogID)
	if !ok || len(entry.BaseURLOptions) == 0 {
		return customProviderFieldPrompt(titlePrefix, "base", data, "", submitPrefix, cancelCommand)
	}
	current := strings.TrimSpace(data.BaseURL)
	items := make([]PickerItem, 0, len(entry.BaseURLOptions))
	for _, option := range entry.BaseURLOptions {
		items = append(items, PickerItem{
			ID:       option.ID,
			Title:    option.Name,
			Info:     option.URL,
			Selected: strings.TrimSpace(option.URL) == current,
			Command:  submitPrefix + strings.TrimSpace(option.URL),
		})
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, titlePrefix+": endpoint").
			Back(cancelCommand).
			Items(items...).
			Ptr(),
	}
}

func customProviderReasoningEffortPickerWithOptions(titlePrefix string, data customProviderForm, efforts []string, submitPrefix string, cancelCommand string) Result {
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, titlePrefix+": reasoning effort").
			HideBack(true).
			Close(cancelCommand).
			Items(customProviderReasoningEffortItems(data, efforts, submitPrefix)...).
			Ptr(),
	}
}

func customProviderReasoningEffortItems(data customProviderForm, efforts []string, prefix string) []PickerItem {
	current := providers.NormalizeReasoningEffort(data.Reasoning)
	if current == "" {
		current = providers.DefaultReasoningEffort
	}
	items := make([]PickerItem, 0, len(efforts))
	for _, effort := range efforts {
		items = append(items, PickerItem{
			ID:       effort,
			Title:    strings.Title(effort),
			Selected: current == effort,
			Command:  prefix + effort,
		})
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

func customProviderFieldPrompt(titlePrefix string, field string, data customProviderForm, placeholder string, submitPrefix string, cancelCommand string) Result {
	if placeholder == "" {
		placeholder = customProviderFieldPlaceholder(field)
	}
	return Result{
		Handled: true,
		Prompt: &PromptData{
			Title:               titlePrefix + ": " + customProviderFieldTitle(field),
			Placeholder:         placeholder,
			Value:               data.field(field),
			SubmitCommandPrefix: submitPrefix,
			CancelCommand:       cancelCommand,
			Sensitive:           field == "key",
		},
	}
}
