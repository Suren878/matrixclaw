package controlplane

import (
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
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
	for len(parts) < 5 {
		parts = append(parts, "")
	}
	return customProviderForm{
		Name:        strings.TrimSpace(parts[0]),
		BaseURL:     strings.TrimSpace(parts[1]),
		Model:       strings.TrimSpace(parts[2]),
		APIKey:      strings.TrimSpace(parts[3]),
		ToolUseMode: providers.ToolUseMode(strings.TrimSpace(parts[4])),
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
	Title              string
	Data               customProviderForm
	KeyStatus          string
	IncludeToolProfile bool
	SubmitCommand      func(token string) string
	CancelCommand      string
	EditCommand        func(field string, token string) string
	Error              string
}

func customProviderFormResult(config customProviderFormResultData) Result {
	token := encodeCustomProviderFormToken(config.Data)
	fields := customProviderFormFields(config.Data, config.KeyStatus, config.IncludeToolProfile, func(field string) string {
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

func customProviderFormFields(data customProviderForm, keyStatus string, includeToolProfile bool, editCommand func(string) string) []FormField {
	fields := []FormField{
		{ID: "name", Label: "Name", Value: fieldStatus(data.Name), EditCommand: editCommand("name")},
		{ID: "base", Label: "Base URL", Value: fieldStatus(data.BaseURL), EditCommand: editCommand("base")},
		{ID: "model", Label: "Model", Value: fieldStatus(data.Model), EditCommand: editCommand("model")},
		{ID: "key", Label: "API Key", Value: keyStatus, EditCommand: editCommand("key")},
	}
	if includeToolProfile {
		fields = append(fields,
			FormField{ID: "tools", Label: "Tool Mode", Value: toolUseModeStatus(data.ToolUseMode), EditCommand: editCommand("tools")},
		)
	}
	return fields
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
		{ID: "native", Title: "Native", Info: "provider tools", Selected: current == providers.ToolUseNative, Command: prefix + string(providers.ToolUseNative)},
		{ID: "disabled", Title: "Disabled", Info: "no tools", Selected: current == providers.ToolUseDisabled, Command: prefix + string(providers.ToolUseDisabled)},
	}
}

func toolUseModeStatus(value providers.ToolUseMode) string {
	switch providers.NormalizeToolUseMode(value) {
	case providers.ToolUseDisabled:
		return "Disabled"
	default:
		return "Native"
	}
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
