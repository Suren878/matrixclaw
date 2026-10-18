package controlplane

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/textutil"
)

func (d *Dispatcher) customProviderDeleteConfirm(ctx context.Context, providerID string) (Result, error) {
	provider, err := d.customSetupProvider(ctx, providerID)
	if err != nil {
		return Result{}, err
	}
	name := textutil.FirstNonEmpty(provider.Name, provider.ID)
	return Result{
		Screen: deleteConfirmData("Delete custom provider `"+name+"`?", providerCustomDrop.command(provider.ID), providerEdit.command(provider.ID)),
	}, nil
}

func (d *Dispatcher) deleteCustomProvider(ctx context.Context, providerID string) (Result, error) {
	provider, err := d.customSetupProvider(ctx, providerID)
	if err != nil {
		return Result{}, err
	}
	if err := d.daemon.DeleteSetupProvider(ctx, provider.ID); err != nil {
		return Result{}, err
	}
	return Result{Screen: TextData{Text: fmt.Sprintf("Provider `%s` deleted.", textutil.FirstNonEmpty(provider.Name, provider.ID))}, ReloadSnapshot: true}, nil
}
