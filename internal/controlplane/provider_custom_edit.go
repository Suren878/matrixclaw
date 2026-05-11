package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) customProviderEditForm(ctx context.Context, providerID string) (Result, error) {
	provider, err := d.customSetupProvider(ctx, providerID)
	if err != nil {
		return Result{}, err
	}
	return customProviderEditFormResult(provider, customProviderForm{
		Name:        firstNonEmptyTrimmed(provider.Name, provider.ID),
		BaseURL:     strings.TrimSpace(provider.BaseURL),
		Model:       strings.TrimSpace(provider.Model),
		ToolUseMode: provider.ToolUseMode,
	}, ""), nil
}

func (d *Dispatcher) handleCustomProviderEditStep(ctx context.Context, step string, form string) (Result, error) {
	switch strings.ToLower(strings.TrimSpace(step)) {
	case "edit-form":
		provider, data, _, err := d.customProviderEditData(ctx, form)
		if err != nil {
			return Result{}, err
		}
		return customProviderEditFormResult(provider, data, ""), nil
	case "edit-field":
		provider, data, field, err := d.customProviderEditData(ctx, form)
		if err != nil {
			return Result{}, err
		}
		providerID := encodeCustomProviderField(provider.ID)
		title := "Edit " + firstNonEmptyTrimmed(provider.Name, provider.ID)
		if strings.TrimSpace(field) == "tools" {
			data = data.withDefaultToolProfile(provider.Type)
			token := encodeCustomProviderFormToken(data)
			return customProviderToolModePicker(title, data, customProviderCommandPrefix("edit-set", "tools", providerID, token), customProviderCommand("edit-form", providerID, token)), nil
		}
		token := encodeCustomProviderFormToken(data)
		placeholder := ""
		if field == "key" {
			placeholder = "leave empty to keep"
		}
		return customProviderFieldPrompt(title, field, data, placeholder, customProviderCommandPrefix("edit-set", field, providerID, token), customProviderCommand("edit-form", providerID, token)), nil
	case "edit-set":
		provider, data, field, err := d.customProviderEditData(ctx, form)
		if err != nil {
			return Result{}, err
		}
		data = data.withField(field, valueAfterFields(form, 3))
		return customProviderEditFormResult(provider, data, ""), nil
	case "edit-save":
		provider, data, _, err := d.customProviderEditData(ctx, form)
		if err != nil {
			return Result{}, err
		}
		if message := data.validationMessage(false); message != "" {
			return customProviderEditFormResult(provider, data, message), nil
		}
		data = data.withDefaultToolProfile(provider.Type)
		return d.saveCustomProviderEdit(ctx, provider, data.Name, data.BaseURL, data.Model, data.APIKey, data.ToolUseMode)
	default:
		return Result{Handled: true, Text: "Unknown custom provider edit step."}, nil
	}
}

func (d *Dispatcher) customProviderEditData(ctx context.Context, raw string) (setup.ProviderSetupItem, customProviderForm, string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 2 {
		return setup.ProviderSetupItem{}, customProviderForm{}, "", fmt.Errorf("provider id and form token are required")
	}
	tokenIndex := 1
	field := ""
	providerField := fields[0]
	if len(fields) >= 3 && isCustomProviderFormField(providerField) {
		field = strings.TrimSpace(fields[0])
		providerField = fields[1]
		tokenIndex = 2
	}
	providerID, err := decodeCustomProviderField(providerField)
	if err != nil {
		return setup.ProviderSetupItem{}, customProviderForm{}, "", err
	}
	provider, err := d.customSetupProvider(ctx, providerID)
	if err != nil {
		return setup.ProviderSetupItem{}, customProviderForm{}, "", err
	}
	data, err := decodeCustomProviderFormToken(fields[tokenIndex])
	if err != nil {
		return setup.ProviderSetupItem{}, customProviderForm{}, "", err
	}
	return provider, data, field, nil
}

func customProviderEditFormResult(provider setup.ProviderSetupItem, data customProviderForm, message string) Result {
	providerID := encodeCustomProviderField(provider.ID)
	return customProviderFormResult(customProviderFormResultData{
		Title:              "Edit " + firstNonEmptyTrimmed(provider.Name, provider.ID),
		Data:               data,
		KeyStatus:          editSecretFieldStatus(provider, data.APIKey),
		IncludeToolProfile: strings.TrimSpace(provider.Type) == providers.TypeOpenAICompat,
		SubmitCommand: func(token string) string {
			return customProviderCommand("edit-save", providerID, token)
		},
		CancelCommand: "/provider " + provider.ID,
		EditCommand: func(field string, token string) string {
			return customProviderCommand("edit-field", field, providerID, token)
		},
		Error: message,
	})
}

func editSecretFieldStatus(provider setup.ProviderSetupItem, value string) string {
	if strings.TrimSpace(value) != "" || strings.TrimSpace(provider.APIKeyPreview) != "" {
		return "Set"
	}
	return "Required"
}

func isCustomProviderFormField(value string) bool {
	switch strings.TrimSpace(value) {
	case "name", "base", "model", "key", "tools":
		return true
	default:
		return false
	}
}

func (d *Dispatcher) saveCustomProviderEdit(ctx context.Context, provider setup.ProviderSetupItem, name string, baseURL string, model string, apiKey string, toolUseMode providers.ToolUseMode) (Result, error) {
	configured, err := d.providers.ConfigureSetupProvider(ctx, provider.ID, setup.ProviderSetupUpdate{
		Name:        name,
		Type:        provider.Type,
		BaseURL:     baseURL,
		Model:       model,
		APIKey:      apiKey,
		ToolUseMode: toolUseMode,
		Active:      false,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: fmt.Sprintf("Provider `%s` updated.", configured.Name), ReloadSnapshot: true}, nil
}
