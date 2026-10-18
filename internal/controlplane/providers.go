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

var (
	providerRoot         = valueVerb(CommandProvider, "", 0)
	providerUse          = verb(CommandProvider, "use", 1)
	providerEdit         = verb(CommandProvider, "edit", 1)
	providerCustom       = verb(CommandProvider, "custom", 0)
	providerCustomNew    = verb(CommandProvider, "custom new", 1)
	providerCustomDelete = verb(CommandProvider, "custom delete", 1)
	providerCustomDrop   = verb(CommandProvider, "custom delete-confirm", 1)
	providerFormShow     = verb(CommandProvider, "form", 1)
	providerFormEdit     = verb(CommandProvider, "form-field", 2)
	providerFormSet      = valueVerb(CommandProvider, "form-set", 2)
	providerFormSave     = verb(CommandProvider, "form-save", 1)
	providerAuth         = verb(CommandProvider, "auth", 1)
	providerAuthComplete = verb(CommandProvider, "auth-complete", 1)
)

func (d *Dispatcher) handleProvider(ctx context.Context, r Route) (Result, error) {
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return textResult("No active session. Use /new or /sessions."), nil
	}
	if core.CoreSessionIsExternalAgent(*session) {
		return textResult("This session uses " + sessionRuntimeLabel(*session) + ". Provider selection is available for MatrixClaw sessions only."), nil
	}
	if !d.owner() && !r.Is(providerUse) && (!r.Is(providerRoot) || r.Value != "") {
		return textResult(ownerOnlySettings), nil
	}
	switch r.Verb {
	case providerUse:
		return d.useProvider(ctx, session, r.arg(0))
	case providerEdit:
		return d.handleProviderEdit(ctx, r.arg(0))
	case providerCustom:
		return customProviderTypePicker(), nil
	case providerCustomNew:
		return d.openCustomProviderForm(r.arg(0)), nil
	case providerCustomDelete:
		return d.customProviderDeleteConfirm(ctx, r.arg(0))
	case providerCustomDrop:
		return d.deleteCustomProvider(ctx, r.arg(0))
	case providerFormShow, providerFormEdit, providerFormSet, providerFormSave:
		return d.handleProviderForm(ctx, session, r)
	case providerAuth:
		return d.handleOpenAICodexAuth(ctx, r.arg(0))
	case providerAuthComplete:
		return d.handleOpenAICodexAuthComplete(ctx, r.arg(0))
	}
	providers, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	if r.Value != "" {
		if provider, ok := findSetupProvider(providers, r.Value); ok {
			return d.openProviderForm(providerForm{Provider: provider}), nil
		}
		return d.useProvider(ctx, session, r.Value)
	}
	return Result{
		Screen: NewPickerData(PickerProvider, "Provider").Command(providerRoot.command()).Items(providerPickerItems(providers, session, d.owner())...).Build(),
	}, nil
}

func (d *Dispatcher) useProvider(ctx context.Context, session *core.Session, providerID string) (Result, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return textResult("Provider id is required."), nil
	}
	updated, err := d.daemon.UpdateSessionProvider(ctx, session.ID, providerID)
	if err != nil {
		return Result{}, err
	}
	return d.providerSelectedResult(ctx, updated)
}

func isOpenAICodexProvider(provider setup.ProviderSetupItem) bool {
	policy := providers.PolicyForProvider(provider.ID, provider.Type)
	return policy.AuthMode == providers.ProviderAuthOAuth && policy.Type == providers.TypeOpenAICodex
}

func openAICodexAuthInfo() string {
	status := openaicodex.CurrentAuthStatus()
	if status.SignedIn {
		return "Signed in"
	}
	if status.Expired {
		return "Expired"
	}
	return "Not signed in"
}

func (d *Dispatcher) providerSelectedResult(ctx context.Context, session core.Session) (Result, error) {
	text := fmt.Sprintf("✅ Provider selected: %s · %s", session.ProviderID, session.ModelID)
	if _, err := d.daemon.CreateSystemMessage(ctx, session.ID, text); err != nil {
		return Result{}, err
	}
	response, err := d.daemon.SessionModels(ctx, session.ID)
	if err == nil {
		picker := sessionModelPicker(session.ID, response)
		if len(picker.Items) > 1 {
			picker.Meta = text + "\nChoose a model."
			return Result{
				Screen:         picker,
				ReloadSnapshot: true,
			}, nil
		}
	}
	return Result{
		Screen:         TextData{Text: text},
		ReloadSnapshot: true,
	}, nil
}
