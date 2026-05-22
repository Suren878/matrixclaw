package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex"
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
	if core.NormalizeSessionRuntime(session.RuntimeID) != core.SessionRuntimeMatrixClaw {
		return Result{Handled: true, Text: "This session uses " + sessionRuntimeLabel(*session) + ". Provider selection is available for MatrixClaw sessions only."}, nil
	}
	value := strings.TrimSpace(args)
	step, rest := firstCommandStep(value)
	switch step {
	case "key":
		return d.handleProviderKey(ctx, session, rest)
	case "custom":
		return d.handleCustomProvider(ctx, session, rest)
	case "edit":
		return d.handleProviderEdit(ctx, session, rest)
	case "auth":
		return d.handleOpenAICodexAuth(ctx, rest)
	case "auth-complete":
		return d.handleOpenAICodexAuthComplete(ctx, rest)
	case "use":
		return d.useProvider(ctx, session, rest)
	}

	providers, err := d.providers.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	if value != "" {
		provider, ok := findSetupProvider(providers, value)
		if ok {
			return providerEditFormResult(provider, formFromProvider(provider), ""), nil
		}
		return d.useProvider(ctx, session, value)
	}

	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerProvider, "Provider").HideBack(true).Items(ProviderPickerItems(providers, session)...).Ptr(),
	}, nil
}

func firstCommandStep(value string) (string, string) {
	token, rest := firstCommandToken(value)
	return strings.ToLower(token), rest
}

func firstCommandToken(value string) (string, string) {
	value = strings.TrimSpace(value)
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "", ""
	}
	token := strings.TrimSpace(fields[0])
	return token, strings.TrimSpace(strings.TrimPrefix(value, token))
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
	return d.providerSelectedResult(ctx, updated)
}

func providerActions(provider setup.ProviderSetupItem, selected bool) Result {
	title := strings.TrimSpace(provider.Name)
	if title == "" {
		title = strings.TrimSpace(provider.ID)
	}
	picker := NewPickerData(PickerProviderActions, title).
		Context(strings.TrimSpace(provider.ID)).
		Back(providerCommand()).
		Item(PickerItem{ID: "use", Title: "Use", Selected: selected, Disabled: !provider.Configured}).
		Row("edit", "Edit", "", providerEditCommand(provider.ID))
	if isOpenAICodexProvider(provider) {
		picker.Row("auth", "Authorization", openAICodexAuthInfo(), providerCommand("auth", providerEncodedID(provider.ID)))
	}
	if isCustomSetupProvider(provider) {
		picker.Danger("delete", "Delete", "")
	}
	return Result{Handled: true, Picker: picker.Ptr()}
}

func isOpenAICodexProvider(provider setup.ProviderSetupItem) bool {
	policy := providerPolicy(provider)
	return policy.AuthMode == providers.ProviderAuthOAuth &&
		policy.RuntimeProviderType == providers.TypeOpenAICodex
}

func providerPolicy(provider setup.ProviderSetupItem) providers.ProviderPolicy {
	return providers.PolicyForProvider(providerFormCatalogID(provider), providerFormType(provider))
}

func openAICodexAuthInfo() string {
	status := openaicodex.CurrentAuthStatus()
	if status.SignedIn {
		if strings.TrimSpace(status.Source) != "" {
			return "Signed in"
		}
		return "Signed in"
	}
	if status.Expired {
		return "Expired"
	}
	return "Not signed in"
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
			return d.providerSelectedResult(ctx, updated)
		}
	}
	text := fmt.Sprintf("✅ Provider configured: %s", configured.Name)
	return Result{
		Handled:        true,
		Text:           text,
		ReloadSnapshot: true,
	}, nil
}

func (d *Dispatcher) providerSelectedResult(ctx context.Context, session core.Session) (Result, error) {
	text := fmt.Sprintf("✅ Provider selected: %s · %s", session.ProviderID, session.ModelID)
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
