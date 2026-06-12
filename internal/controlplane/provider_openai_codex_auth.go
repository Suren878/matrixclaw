package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex"
)

func (d *Dispatcher) handleOpenAICodexAuth(ctx context.Context, args string) (Result, error) {
	providerID, err := decodeCustomProviderField(firstField(args))
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(providerID) == "" {
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
	if !isOpenAICodexProvider(provider) {
		return Result{Handled: true, Text: "Authorization is only available for OpenAI Codex Subscription."}, nil
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
		Handled: true,
		Picker: NewPickerData(PickerProviderActions, "OpenAI Codex Authorization").
			Meta("Open "+device.VerifyURL+" · Code "+device.UserCode).
			Back(providerEditCommand(provider.ID)).
			Row("complete", "Complete sign-in", "After approving in browser", providerCommand("auth-complete", providerEncodedID(provider.ID), token)).
			Ptr(),
	}, nil
}

func (d *Dispatcher) handleOpenAICodexAuthComplete(ctx context.Context, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	providerID, err := decodeCustomProviderField(providerID)
	if err != nil {
		return Result{}, err
	}
	token := firstField(rest)
	if strings.TrimSpace(providerID) == "" || strings.TrimSpace(token) == "" {
		return Result{Handled: true, Text: "OpenAI Codex sign-in session is missing."}, nil
	}
	providers, err := d.providers.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	provider, found := findSetupProvider(providers, providerID)
	if !found {
		return Result{Handled: true, Text: "Unknown provider: " + providerID}, nil
	}
	if !isOpenAICodexProvider(provider) {
		return Result{Handled: true, Text: "Authorization is only available for OpenAI Codex Subscription."}, nil
	}
	device, err := openaicodex.DecodeLoginDevice(token)
	if err != nil {
		return Result{}, err
	}
	if _, err := openaicodex.CompleteDeviceLogin(ctx, nil, device); err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Info: &InfoData{
			Title: "OpenAI Codex Authorization",
			Text:  "Login successful.",
		},
	}, nil
}
