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
		return customProviderFormPicker(kind, typeLabel, setup.ProviderFormState{}.WithDefaultToolProfile(providerType), ""), nil
	}
	if result, ok, err := d.handleCustomProviderCreateStep(ctx, session, kind, providerType, typeLabel, form); ok || err != nil {
		return result, err
	}
	return customProviderFormPicker(kind, typeLabel, setup.ProviderFormState{}, ""), nil
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
		data = data.WithDefaultToolProfile(providerType)
		return customProviderFormPicker(kind, label, data, ""), true, nil
	}
	if field, ok := customProviderStepField(step, "edit-"); ok {
		data, err := decodeCustomProviderFormToken(firstField(value))
		if err != nil {
			return Result{}, true, err
		}
		if field == "tools" {
			data = data.WithDefaultToolProfile(providerType)
			token := encodeCustomProviderFormToken(data)
			return customProviderToolModePicker("Custom "+label, data, customProviderCommandPrefix(kind, "set-tools", token), customProviderCommand(kind, "form", token)), true, nil
		}
		if field == "reasoning" {
			data = data.WithDefaultProviderOptions(providers.ProviderCapabilities("", providerType))
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
		data = data.WithField(field, fieldValue)
		if field == "tools" {
			data = data.WithDefaultToolProfile(providerType)
		}
		return customProviderFormPicker(kind, label, data, ""), true, nil
	}

	switch step {
	case "save":
		data, err := decodeCustomProviderFormToken(firstField(value))
		if err != nil {
			return Result{}, true, err
		}
		data = data.WithDefaultToolProfile(providerType)
		if message := data.ValidationMessage(true); message != "" {
			return customProviderFormPicker(kind, label, data, message), true, nil
		}
		return customProviderSaveConfirm(kind, data), true, nil
	case "save-confirm":
		data, err := decodeCustomProviderFormToken(firstField(value))
		if err != nil {
			return Result{}, true, err
		}
		if message := data.ValidationMessage(true); message != "" {
			return customProviderFormPicker(kind, label, data, message), true, nil
		}
		data = data.WithDefaultToolProfile(providerType)
		result, err := d.saveCustomProvider(ctx, session, providerType, data)
		return result, true, err
	default:
		return Result{}, false, nil
	}
}

func (d *Dispatcher) saveCustomProvider(ctx context.Context, session *core.Session, providerType string, data setup.ProviderFormState) (Result, error) {
	providerID := customProviderID(data.Name)
	if providerID == "" {
		return Result{Handled: true, Text: "Provider name is required."}, nil
	}
	update := data.WithDefaultProviderOptions(providers.ProviderCapabilities("", providerType)).ToSetupUpdate(providerType, true)
	configured, err := d.providers.ConfigureSetupProvider(ctx, providerID, update)
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

func customProviderFormPicker(kind string, label string, data setup.ProviderFormState, message string) Result {
	capabilities := providers.Capabilities{}
	if providerType, _, ok := customProviderType(kind); ok {
		capabilities = providers.ProviderCapabilities("", providerType)
		data = data.WithDefaultProviderOptions(capabilities)
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

func customProviderSaveConfirm(kind string, data setup.ProviderFormState) Result {
	token := encodeCustomProviderFormToken(data)
	return Result{
		Handled: true,
		Confirm: &ConfirmData{
			Message:        "Save custom provider `" + data.Name + "`?",
			ConfirmLabel:   "Save",
			CancelLabel:    "Close",
			ConfirmCommand: customProviderCommand(kind, "save-confirm", token),
			CancelCommand:  customProviderCommand(kind, "form", token),
		},
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

func secretFieldStatus(value string) string {
	if value := strings.TrimSpace(value); value != "" {
		return setup.MaskSecret(value)
	}
	return "Required"
}
