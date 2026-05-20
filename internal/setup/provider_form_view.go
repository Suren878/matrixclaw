package setup

import "strings"

type ProviderFormViewOptions struct {
	APIKeyStatus       string
	ShowRequiredStatus bool
}

type ProviderFormViewField struct {
	ID              ProviderFormFieldID
	CommandID       string
	Label           string
	Status          string
	RequiredMessage string
	Sensitive       bool
	Picker          bool
	Accent          bool
}

func ProviderFormViewFields(spec ProviderFormSpec, options ProviderFormViewOptions) []ProviderFormViewField {
	fields := make([]ProviderFormViewField, 0, len(spec.Fields))
	for _, field := range spec.Fields {
		status := strings.TrimSpace(field.Status)
		if field.ID == ProviderFormFieldAPIKey && strings.TrimSpace(options.APIKeyStatus) != "" {
			status = strings.TrimSpace(options.APIKeyStatus)
		}
		if field.Required && options.ShowRequiredStatus {
			status = ProviderRequiredStatus(status)
		}
		fields = append(fields, ProviderFormViewField{
			ID:              field.ID,
			CommandID:       ProviderFormCommandID(field.ID),
			Label:           field.Label,
			Status:          status,
			RequiredMessage: ProviderRequiredMessage(field),
			Sensitive:       field.Sensitive,
			Picker:          field.Picker,
			Accent:          field.ID == ProviderFormFieldModel && strings.TrimSpace(field.Status) != "",
		})
	}
	return fields
}

func ProviderRequiredStatus(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return "Required"
}

func ProviderRequiredMessage(field ProviderFormField) string {
	if !field.Required {
		return ""
	}
	switch field.ID {
	case ProviderFormFieldName:
		return "provider name is required"
	case ProviderFormFieldBaseURL:
		return "provider base URL is required"
	case ProviderFormFieldAPIKey:
		return "provider API key is required"
	case ProviderFormFieldModel:
		return "provider model is required"
	default:
		return ""
	}
}

func ProviderFormCommandID(id ProviderFormFieldID) string {
	switch id {
	case ProviderFormFieldBaseURL:
		return "base"
	case ProviderFormFieldAPIKey:
		return "key"
	case ProviderFormFieldReasoningEffort:
		return "reasoning"
	case ProviderFormFieldToolUse:
		return "tools"
	default:
		return string(id)
	}
}

func ProviderFormFieldIDFromCommand(value string) (ProviderFormFieldID, bool) {
	switch strings.TrimSpace(value) {
	case string(ProviderFormFieldName):
		return ProviderFormFieldName, true
	case "base", string(ProviderFormFieldBaseURL):
		return ProviderFormFieldBaseURL, true
	case "key", string(ProviderFormFieldAPIKey):
		return ProviderFormFieldAPIKey, true
	case string(ProviderFormFieldModel):
		return ProviderFormFieldModel, true
	case "reasoning", string(ProviderFormFieldReasoningEffort):
		return ProviderFormFieldReasoningEffort, true
	case "tools", string(ProviderFormFieldToolUse):
		return ProviderFormFieldToolUse, true
	default:
		return "", false
	}
}
