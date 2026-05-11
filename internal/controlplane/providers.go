package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleProvider(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.providers == nil {
		return unsupportedRuntime("provider"), nil
	}
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	_, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "No active session. Use /new or /sessions."}, nil
	}
	value := strings.TrimSpace(args)
	if strings.HasPrefix(value, "key ") {
		return d.handleProviderKey(ctx, session, strings.TrimSpace(strings.TrimPrefix(value, "key ")))
	}
	if strings.HasPrefix(value, "custom") {
		return d.handleCustomProvider(ctx, session, strings.TrimSpace(strings.TrimPrefix(value, "custom")))
	}
	if strings.HasPrefix(value, "use ") {
		return d.useProvider(ctx, session, strings.TrimSpace(strings.TrimPrefix(value, "use ")))
	}

	providers, err := d.providers.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	if value != "" {
		provider, ok := findSetupProvider(providers, value)
		if ok && provider.Configured && isCustomSetupProvider(provider) {
			return customProviderActions(provider, strings.EqualFold(strings.TrimSpace(provider.ID), strings.TrimSpace(session.ProviderID))), nil
		}
		if ok && !provider.Configured {
			return providerKeyPrompt(provider), nil
		}
		return d.useProvider(ctx, session, value)
	}

	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerProvider, "Provider").HideBack(true).Items(ProviderPickerItems(providers, session)...).Ptr(),
	}, nil
}

func (d *Dispatcher) useProvider(ctx context.Context, session *core.Session, providerID string) (Result, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return Result{Handled: true, Text: "Provider id is required."}, nil
	}
	updated, err := d.providers.UpdateSessionProvider(ctx, session.ID, providerID)
	if err != nil {
		return Result{}, err
	}
	text := fmt.Sprintf("✅ Provider selected: %s · %s", updated.ProviderID, updated.ModelID)
	if d.messages != nil {
		if _, err := d.messages.CreateSystemMessage(ctx, updated.ID, text); err != nil {
			return Result{}, err
		}
	}
	return Result{
		Handled:        true,
		Text:           text,
		ReloadSnapshot: true,
	}, nil
}

func customProviderActions(provider setup.ProviderSetupItem, selected bool) Result {
	title := strings.TrimSpace(provider.Name)
	if title == "" {
		title = strings.TrimSpace(provider.ID)
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderActions, title).
			Context(strings.TrimSpace(provider.ID)).
			Back("/provider").
			Item(PickerItem{ID: "use", Title: "Use", Selected: selected}).
			Row("edit", "Edit", "").
			Danger("delete", "Delete", "").
			Ptr(),
	}
}

func isCustomSetupProvider(provider setup.ProviderSetupItem) bool {
	if !provider.Configured {
		return false
	}
	id := providers.NormalizeProviderID(provider.CatalogID)
	if id == "" {
		id = providers.NormalizeProviderID(provider.ID)
	}
	for _, entry := range providers.Catalog() {
		if providers.NormalizeProviderID(entry.ID) == id {
			return false
		}
	}
	return true
}

func (d *Dispatcher) handleModel(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.providers == nil {
		return unsupportedRuntime("provider"), nil
	}
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	_, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "No active session. Use /new or /sessions."}, nil
	}
	if value := strings.TrimSpace(args); value != "" {
		session, err := d.providers.UpdateSessionModel(ctx, session.ID, value)
		if err != nil {
			return Result{}, err
		}
		text := fmt.Sprintf("✅ Model selected: %s", session.ModelID)
		if d.messages != nil {
			if _, err := d.messages.CreateSystemMessage(ctx, session.ID, text); err != nil {
				return Result{}, err
			}
		}
		return Result{
			Handled:        true,
			Text:           text,
			ReloadSnapshot: true,
		}, nil
	}

	providerID, modelID, models, err := d.providers.ModelsForSession(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	pickerItems := make([]PickerItem, 0, len(models)+1)
	for _, model := range models {
		pickerItems = append(pickerItems, PickerItem{
			ID:       model,
			Title:    model,
			Selected: strings.TrimSpace(model) == strings.TrimSpace(modelID),
		})
	}
	pickerItems = append(pickerItems, CloseItem())
	title := "Model"
	if strings.TrimSpace(providerID) != "" {
		title = "Model: " + strings.TrimSpace(providerID)
	}
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerModel, title).HideBack(true).Items(pickerItems...).Ptr(),
	}, nil
}

func (d *Dispatcher) handleProviderKey(ctx context.Context, session *core.Session, args string) (Result, error) {
	providerID, apiKey, ok := strings.Cut(strings.TrimSpace(args), " ")
	providerID = strings.TrimSpace(providerID)
	apiKey = strings.TrimSpace(apiKey)
	if providerID == "" {
		return Result{Handled: true, Text: "Provider id is required."}, nil
	}
	providers, err := d.providers.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	provider, found := findSetupProvider(providers, providerID)
	if !found {
		return Result{Handled: true, Text: "Unknown provider: " + providerID}, nil
	}
	if apiKey == "" || !ok {
		return providerKeyPrompt(provider), nil
	}
	configured, err := d.providers.ConfigureSetupProvider(ctx, providerID, setup.ProviderSetupUpdate{
		APIKey: apiKey,
		Active: true,
	})
	if err != nil {
		return Result{}, err
	}
	if session != nil {
		updated, updateErr := d.providers.UpdateSessionProvider(ctx, session.ID, configured.ID)
		if updateErr == nil {
			text := fmt.Sprintf("✅ Provider selected: %s · %s", updated.ProviderID, updated.ModelID)
			if d.messages != nil {
				if _, err := d.messages.CreateSystemMessage(ctx, updated.ID, text); err != nil {
					return Result{}, err
				}
			}
			return Result{
				Handled:        true,
				Text:           text,
				ReloadSnapshot: true,
			}, nil
		}
	}
	text := fmt.Sprintf("✅ Provider configured: %s", configured.Name)
	return Result{
		Handled:        true,
		Text:           text,
		ReloadSnapshot: true,
	}, nil
}
