package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleCustomProviderCreate(ctx context.Context, session *core.Session, args string, kind string) (Result, error) {
	providerType, typeLabel, ok := customProviderType(kind)
	if !ok {
		return Result{Handled: true, Text: "Choose OpenAI-Compatible or Anthropic-Compatible."}, nil
	}
	form := strings.TrimSpace(strings.TrimPrefix(args, kind))
	if form == "" {
		return customProviderFormPicker(kind, typeLabel, customProviderForm{}.withDefaultToolProfile(providerType), ""), nil
	}
	if result, ok, err := d.handleCustomProviderCreateStep(ctx, session, kind, providerType, typeLabel, form); ok || err != nil {
		return result, err
	}
	return customProviderFormPicker(kind, typeLabel, customProviderForm{}, ""), nil
}

func customProviderType(value string) (string, string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "openai", "openai-compatible", "openaicompat":
		return providers.TypeOpenAICompat, "OpenAI-Compatible", true
	case "anthropic", "anthropic-compatible", "anthropiccompat":
		return providers.TypeAnthropic, "Anthropic-Compatible", true
	default:
		return "", "", false
	}
}

func (d *Dispatcher) handleCustomProviderCreateStep(ctx context.Context, session *core.Session, kind string, providerType string, label string, form string) (Result, bool, error) {
	parts := strings.Fields(form)
	if len(parts) == 0 {
		return Result{}, false, nil
	}
	step := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(strings.TrimPrefix(form, parts[0]))

	if step == "form" {
		data, err := decodeCustomProviderFormToken(firstField(value))
		if err != nil {
			return Result{}, true, err
		}
		data = data.withDefaultToolProfile(providerType)
		return customProviderFormPicker(kind, label, data, ""), true, nil
	}
	if field, ok := customProviderStepField(step, "edit-"); ok {
		data, err := decodeCustomProviderFormToken(firstField(value))
		if err != nil {
			return Result{}, true, err
		}
		if field == "tools" {
			data = data.withDefaultToolProfile(providerType)
			token := encodeCustomProviderFormToken(data)
			return customProviderToolModePicker("Custom "+label, data, customProviderCommandPrefix(kind, "set-tools", token), customProviderCommand(kind, "form", token)), true, nil
		}
		if field == "reasoning" {
			data = data.withDefaultProviderOptions(providers.ProviderCapabilities("", providerType))
			token := encodeCustomProviderFormToken(data)
			return customProviderReasoningEffortPickerWithOptions(
				"Custom "+label,
				data,
				providers.ReasoningEffortsForProvider("", providerType),
				customProviderCommandPrefix(kind, "set-reasoning", token),
				customProviderCommand(kind, "form", token),
			), true, nil
		}
		token := encodeCustomProviderFormToken(data)
		return customProviderFieldPrompt("Custom "+label, field, data, "", customProviderCommandPrefix(kind, "set-"+field, token), customProviderCommand(kind, "form", token)), true, nil
	}
	if field, ok := customProviderStepField(step, "set-"); ok {
		token := firstField(value)
		data, err := decodeCustomProviderFormToken(token)
		if err != nil {
			return Result{}, true, err
		}
		fieldValue := strings.TrimSpace(strings.TrimPrefix(value, token))
		data = data.withField(field, fieldValue)
		if field == "tools" {
			data = data.withDefaultToolProfile(providerType)
		}
		return customProviderFormPicker(kind, label, data, ""), true, nil
	}

	switch step {
	case "save":
		data, err := decodeCustomProviderFormToken(firstField(value))
		if err != nil {
			return Result{}, true, err
		}
		data = data.withDefaultToolProfile(providerType)
		if message := data.validationMessage(true); message != "" {
			return customProviderFormPicker(kind, label, data, message), true, nil
		}
		return customProviderSaveConfirm(kind, data), true, nil
	case "save-confirm":
		data, err := decodeCustomProviderFormToken(firstField(value))
		if err != nil {
			return Result{}, true, err
		}
		if message := data.validationMessage(true); message != "" {
			return customProviderFormPicker(kind, label, data, message), true, nil
		}
		data = data.withDefaultToolProfile(providerType)
		result, err := d.saveCustomProvider(ctx, session, providerType, data.Name, data.BaseURL, data.Model, data.APIKey, data.Reasoning, data.ToolUseMode)
		return result, true, err
	default:
		return Result{}, false, nil
	}
}

func (d *Dispatcher) saveCustomProvider(ctx context.Context, session *core.Session, providerType string, name string, baseURL string, model string, apiKey string, reasoningEffort string, toolUseMode providers.ToolUseMode) (Result, error) {
	providerID := customProviderID(name)
	if providerID == "" {
		return Result{Handled: true, Text: "Provider name is required."}, nil
	}
	capabilities := providers.ProviderCapabilities("", providerType)
	form := customProviderForm{
		Name:        name,
		BaseURL:     baseURL,
		Model:       model,
		APIKey:      apiKey,
		Reasoning:   reasoningEffort,
		ToolUseMode: toolUseMode,
	}.withDefaultProviderOptions(capabilities)
	configured, err := d.providers.ConfigureSetupProvider(ctx, providerID, setup.ProviderSetupUpdate{
		Name:            form.Name,
		Type:            providerType,
		BaseURL:         form.BaseURL,
		Model:           form.Model,
		APIKey:          form.APIKey,
		ReasoningEffort: form.Reasoning,
		ToolUseMode:     form.ToolUseMode,
		Active:          true,
	})
	if err != nil {
		return Result{}, err
	}
	if session != nil {
		updated, updateErr := d.providers.UpdateSessionProvider(ctx, session.ID, configured.ID)
		if updateErr == nil {
			return Result{
				Handled:        true,
				Text:           fmt.Sprintf("Session `%s` now uses provider `%s` with model `%s`.", updated.Title, updated.ProviderID, updated.ModelID),
				ReloadSnapshot: true,
			}, nil
		}
	}
	return Result{Handled: true, Text: fmt.Sprintf("Provider `%s` configured.", configured.Name), ReloadSnapshot: true}, nil
}

type customProviderForm struct {
	Name        string
	BaseURL     string
	Model       string
	APIKey      string
	Reasoning   string
	ToolUseMode providers.ToolUseMode
}

func customProviderFormPicker(kind string, label string, data customProviderForm, message string) Result {
	capabilities := providers.Capabilities{}
	if providerType, _, ok := customProviderType(kind); ok {
		capabilities = providers.ProviderCapabilities("", providerType)
		data = data.withDefaultProviderOptions(capabilities)
	}
	return customProviderFormResult(customProviderFormResultData{
		Title:                  "Custom " + label,
		Data:                   data,
		KeyStatus:              secretFieldStatus(data.APIKey),
		IncludeIdentity:        true,
		IncludeReasoningEffort: capabilities.ReasoningEffort,
		IncludeToolProfile:     capabilities.ToolCalling,
		SubmitCommand: func(token string) string {
			return customProviderCommand(kind, "save", token)
		},
		CancelCommand: customProviderCommand(),
		EditCommand: func(field string, token string) string {
			return customProviderCommand(kind, "edit-"+field, token)
		},
		Error: message,
	})
}

func customProviderSaveConfirm(kind string, data customProviderForm) Result {
	token := encodeCustomProviderFormToken(data)
	return Result{
		Handled: true,
		Confirm: &ConfirmData{
			Message:        "Save custom provider `" + data.Name + "`?",
			ConfirmLabel:   "Save",
			CancelLabel:    "Cancel",
			ConfirmCommand: customProviderCommand(kind, "save-confirm", token),
			CancelCommand:  customProviderCommand(kind, "form", token),
		},
	}
}

func (data customProviderForm) field(field string) string {
	switch field {
	case "name":
		return data.Name
	case "base":
		return data.BaseURL
	case "model":
		return data.Model
	case "key":
		return data.APIKey
	case "reasoning":
		return data.Reasoning
	case "tools":
		return string(data.ToolUseMode)
	default:
		return ""
	}
}

func (data customProviderForm) withField(field string, value string) customProviderForm {
	value = strings.TrimSpace(value)
	switch field {
	case "name":
		data.Name = value
	case "base":
		data.BaseURL = value
	case "model":
		data.Model = value
	case "key":
		data.APIKey = value
	case "reasoning":
		data.Reasoning = providers.NormalizeReasoningEffort(value)
	case "tools":
		data.ToolUseMode = providers.NormalizeToolUseMode(providers.ToolUseMode(value))
	}
	return data
}

func (data customProviderForm) withDefaultToolProfile(providerType string) customProviderForm {
	return data.withDefaultProviderOptions(providers.ProviderCapabilities("", providerType))
}

func (data customProviderForm) withDefaultProviderOptions(capabilities providers.Capabilities) customProviderForm {
	if capabilities.ReasoningEffort {
		if effort := providers.NormalizeReasoningEffort(data.Reasoning); effort != "" {
			data.Reasoning = effort
		} else {
			data.Reasoning = providers.DefaultReasoningEffort
		}
	} else {
		data.Reasoning = ""
	}
	if !capabilities.ToolCalling {
		data.ToolUseMode = ""
		return data
	}
	if strings.TrimSpace(string(data.ToolUseMode)) == "" {
		data.ToolUseMode = providers.ToolUseNative
	}
	return data
}

func (data customProviderForm) validationMessage(requireAPIKey bool) string {
	switch {
	case strings.TrimSpace(data.Name) == "":
		return "Provider name is required."
	case strings.TrimSpace(data.BaseURL) == "":
		return "Base URL is required."
	case strings.TrimSpace(data.Model) == "":
		return "Model is required."
	case requireAPIKey && strings.TrimSpace(data.APIKey) == "":
		return "API key is required."
	default:
		return ""
	}
}

func customProviderFieldTitle(field string) string {
	switch field {
	case "base":
		return "base URL"
	case "key":
		return "API key"
	case "reasoning":
		return "reasoning effort"
	default:
		return field
	}
}

func customProviderFieldPlaceholder(field string) string {
	switch field {
	case "name":
		return "Local AI"
	case "base":
		return "https://api.example.com/v1"
	case "model":
		return "model-id"
	case "key":
		return "API key"
	case "reasoning":
		return "medium"
	default:
		return ""
	}
}

func fieldStatus(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return "Required"
}

func secretFieldStatus(value string) string {
	if value := strings.TrimSpace(value); value != "" {
		return setup.MaskSecret(value)
	}
	return "Required"
}
