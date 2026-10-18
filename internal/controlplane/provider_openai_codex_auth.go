package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex"
)

func (d *Dispatcher) handleOpenAICodexAuth(ctx context.Context, args string) (Result, error) {
	providerID, err := decodeProviderID(firstField(args))
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(providerID) == "" {
		return textResult("Provider id is required."), nil
	}
	providers, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	provider, found := findSetupProvider(providers, providerID)
	if !found {
		return textResult("Unknown provider: " + providerID), nil
	}
	if !isOpenAICodexProvider(provider) {
		return textResult("Authorization is only available for OpenAI Codex Subscription."), nil
	}
	device, err := openaicodex.StartDeviceLogin(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	token, err := openaicodex.EncodeLoginDevice(device)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Screen: NewPickerData(PickerProviderActions, "OpenAI Codex Authorization").
			Meta("Open "+device.VerifyURL+" · Code "+device.UserCode).
			Back(providerEditCommand(provider.ID)).
			Row("complete", "Complete sign-in", "After approving in browser", providerCommand("auth-complete", providerEncodedID(provider.ID), token)).
			Build(),
	}, nil
}

func (d *Dispatcher) handleOpenAICodexAuthComplete(ctx context.Context, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	providerID, err := decodeProviderID(providerID)
	if err != nil {
		return Result{}, err
	}
	token := firstField(rest)
	if strings.TrimSpace(providerID) == "" || strings.TrimSpace(token) == "" {
		return textResult("OpenAI Codex sign-in session is missing."), nil
	}
	providers, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	provider, found := findSetupProvider(providers, providerID)
	if !found {
		return textResult("Unknown provider: " + providerID), nil
	}
	if !isOpenAICodexProvider(provider) {
		return textResult("Authorization is only available for OpenAI Codex Subscription."), nil
	}
	device, err := openaicodex.DecodeLoginDevice(token)
	if err != nil {
		return Result{}, err
	}
	if _, err := openaicodex.CompleteDeviceLogin(ctx, nil, device); err != nil {
		return Result{}, err
	}
	return Result{
		Screen: InfoData{
			Title: "OpenAI Codex Authorization",
			Text:  "Login successful.",
		},
	}, nil
}
