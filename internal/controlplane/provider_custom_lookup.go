package controlplane

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) customSetupProvider(ctx context.Context, providerID string) (setup.ProviderSetupItem, error) {
	providerID, err := decodeCustomProviderField(providerID)
	if err != nil {
		return setup.ProviderSetupItem{}, err
	}
	if d.providers == nil {
		return setup.ProviderSetupItem{}, fmt.Errorf("provider runtime is not configured")
	}
	providers, err := d.providers.ListSetupProviders(ctx)
	if err != nil {
		return setup.ProviderSetupItem{}, err
	}
	provider, ok := findSetupProvider(providers, providerID)
	if !ok {
		return setup.ProviderSetupItem{}, fmt.Errorf("provider %q was not found", providerID)
	}
	if !isCustomSetupProvider(provider) {
		return setup.ProviderSetupItem{}, fmt.Errorf("provider %q is built in and cannot be edited here", provider.Name)
	}
	return provider, nil
}
