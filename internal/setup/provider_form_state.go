package setup

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type ProviderFormState struct {
	Name            string
	BaseURL         string
	Model           string
	APIKey          string
	ReasoningEffort string
	ToolUseMode     providers.ToolUseMode
}

func (state ProviderFormState) Field(commandID string) string {
	fieldID, ok := ProviderFormFieldIDFromCommand(commandID)
	if !ok {
		return ""
	}
	switch fieldID {
	case ProviderFormFieldName:
		return state.Name
	case ProviderFormFieldBaseURL:
		return state.BaseURL
	case ProviderFormFieldModel:
		return state.Model
	case ProviderFormFieldAPIKey:
		return state.APIKey
	case ProviderFormFieldReasoningEffort:
		return state.ReasoningEffort
	case ProviderFormFieldToolUse:
		return string(state.ToolUseMode)
	default:
		return ""
	}
}

func (state ProviderFormState) WithField(commandID string, value string) ProviderFormState {
	value = strings.TrimSpace(value)
	fieldID, ok := ProviderFormFieldIDFromCommand(commandID)
	if !ok {
		return state
	}
	switch fieldID {
	case ProviderFormFieldName:
		state.Name = value
	case ProviderFormFieldBaseURL:
		state.BaseURL = value
	case ProviderFormFieldModel:
		state.Model = value
	case ProviderFormFieldAPIKey:
		state.APIKey = value
	case ProviderFormFieldReasoningEffort:
		state.ReasoningEffort = providers.NormalizeReasoningEffort(value)
	case ProviderFormFieldToolUse:
		state.ToolUseMode = providers.NormalizeToolUseMode(providers.ToolUseMode(value))
	}
	return state
}

func (state ProviderFormState) WithDefaultToolProfile(providerType string) ProviderFormState {
	return state.WithDefaultProviderOptions(providers.ProviderCapabilities("", providerType))
}

func (state ProviderFormState) WithDefaultProviderOptions(capabilities providers.Capabilities) ProviderFormState {
	if capabilities.ReasoningEffort {
		if effort := providers.NormalizeReasoningEffort(state.ReasoningEffort); effort != "" {
			state.ReasoningEffort = effort
		} else {
			state.ReasoningEffort = providers.DefaultReasoningEffort
		}
	} else {
		state.ReasoningEffort = ""
	}
	if !capabilities.ToolCalling {
		state.ToolUseMode = ""
		return state
	}
	if strings.TrimSpace(string(state.ToolUseMode)) == "" {
		state.ToolUseMode = providers.ToolUseNative
	}
	return state
}

func (state ProviderFormState) ValidationMessage(requireAPIKey bool) string {
	switch {
	case strings.TrimSpace(state.Name) == "":
		return "Provider name is required."
	case strings.TrimSpace(state.BaseURL) == "":
		return "Base URL is required."
	case strings.TrimSpace(state.Model) == "":
		return "Model is required."
	case requireAPIKey && strings.TrimSpace(state.APIKey) == "":
		return "API key is required."
	default:
		return ""
	}
}

func (state ProviderFormState) ToSetupUpdate(providerType string, active bool) ProviderSetupUpdate {
	return ProviderSetupUpdate{
		Name:            state.Name,
		Type:            providerType,
		BaseURL:         state.BaseURL,
		Model:           state.Model,
		APIKey:          state.APIKey,
		ReasoningEffort: state.ReasoningEffort,
		ToolUseMode:     state.ToolUseMode,
		Active:          active,
	}
}
