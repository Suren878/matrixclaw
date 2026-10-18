package controlplane

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex"
	"github.com/Suren878/matrixclaw/internal/setup"
)

// codexLogin is a device sign-in in progress, kept in the form store.
type codexLogin struct {
	providerID string
	device     openaicodex.LoginDevice
}

func (d *Dispatcher) handleOpenAICodexAuth(ctx context.Context, providerID string) (Result, error) {
	provider, refusal, err := d.codexProvider(ctx, providerID)
	if err != nil || refusal != "" {
		return textResult(refusal), err
	}
	device, err := openaicodex.StartDeviceLogin(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	loginID := d.forms.open(codexLogin{providerID: provider.ID, device: device})
	return Result{Screen: NewPickerData(PickerProviderActions, "OpenAI Codex Authorization").
		Meta("Open "+device.VerifyURL+" · Code "+device.UserCode).
		Back(providerEdit.command(provider.ID)).
		Row("complete", "Complete sign-in", "After approving in browser", providerAuthComplete.command(loginID)).
		Build()}, nil
}

func (d *Dispatcher) handleOpenAICodexAuthComplete(ctx context.Context, loginID string) (Result, error) {
	login, ok := formValue[codexLogin](d.forms, loginID)
	if !ok {
		return textResult("This OpenAI Codex sign-in has expired. Start it again from the provider."), nil
	}
	if _, refusal, err := d.codexProvider(ctx, login.providerID); err != nil || refusal != "" {
		return textResult(refusal), err
	}
	if _, err := openaicodex.CompleteDeviceLogin(ctx, nil, login.device); err != nil {
		return Result{}, err
	}
	d.forms.close(loginID)
	return Result{Screen: InfoData{Title: "OpenAI Codex Authorization", Text: "Login successful."}}, nil
}

// codexProvider is the configured OpenAI Codex provider providerID names;
// refusal says why there is none.
func (d *Dispatcher) codexProvider(ctx context.Context, providerID string) (setup.ProviderSetupItem, string, error) {
	if providerID == "" {
		return setup.ProviderSetupItem{}, "Provider id is required.", nil
	}
	providers, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return setup.ProviderSetupItem{}, "", err
	}
	provider, found := findSetupProvider(providers, providerID)
	if !found {
		return setup.ProviderSetupItem{}, "Unknown provider: " + providerID, nil
	}
	if !isOpenAICodexProvider(provider) {
		return setup.ProviderSetupItem{}, "Authorization is only available for OpenAI Codex Subscription.", nil
	}
	return provider, "", nil
}
