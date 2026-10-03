package controlplane

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/providers"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) customSetupProvider(ctx context.Context, providerID string) (setup.ProviderSetupItem, error) {
	provider, err := d.setupProvider(ctx, providerID)
	if err != nil {
		return setup.ProviderSetupItem{}, err
	}
	if !provider.Configured || providers.PolicyForProvider(provider.ID, provider.Type).Known {
		return setup.ProviderSetupItem{}, fmt.Errorf("provider %q is built in and cannot be edited here", provider.Name)
	}
	return provider, nil
}

func (d *Dispatcher) setupProvider(ctx context.Context, providerID string) (setup.ProviderSetupItem, error) {
	providerID, err := decodeProviderID(providerID)
	if err != nil {
		return setup.ProviderSetupItem{}, err
	}
	providers, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return setup.ProviderSetupItem{}, err
	}
	provider, ok := findSetupProvider(providers, providerID)
	if !ok {
		return setup.ProviderSetupItem{}, fmt.Errorf("provider %q was not found", providerID)
	}
	return provider, nil
}
