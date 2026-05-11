package controlplane

import (
	"context"
	"fmt"
)

func (d *Dispatcher) customProviderDeleteConfirm(ctx context.Context, providerID string) (Result, error) {
	provider, err := d.customSetupProvider(ctx, providerID)
	if err != nil {
		return Result{}, err
	}
	name := firstNonEmptyTrimmed(provider.Name, provider.ID)
	return Result{
		Handled: true,
		Confirm: deleteConfirmData("Delete custom provider `"+name+"`?", customProviderCommand("delete-confirm", encodeCustomProviderField(provider.ID)), "/provider "+provider.ID),
	}, nil
}

func (d *Dispatcher) deleteCustomProvider(ctx context.Context, providerID string) (Result, error) {
	provider, err := d.customSetupProvider(ctx, providerID)
	if err != nil {
		return Result{}, err
	}
	if err := d.providers.DeleteSetupProvider(ctx, provider.ID); err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: fmt.Sprintf("Provider `%s` deleted.", firstNonEmptyTrimmed(provider.Name, provider.ID)), ReloadSnapshot: true}, nil
}
