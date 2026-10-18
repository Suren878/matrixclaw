package controlplane

import "context"

var (
	storageRootVerb                = verb(CommandModules, "storage", 0)
	storageFilesVerb               = verb(CommandModules, "storage files", 0)
	storageFileVerb                = verb(CommandModules, "storage file", 1)
	storageReadVerb                = verb(CommandModules, "storage read", 1)
	storageDeleteVerb              = verb(CommandModules, "storage delete", 1)
	storageDeleteConfirmVerb       = verb(CommandModules, "storage delete-confirm", 1)
	storageClearVerb               = verb(CommandModules, "storage clear", 0)
	storageClearConfirmVerb        = verb(CommandModules, "storage clear-confirm", 0)
	storageTempVerb                = verb(CommandModules, "storage temp", 0)
	storageTempFileVerb            = verb(CommandModules, "storage temp-file", 1)
	storageTempPromoteVerb         = verb(CommandModules, "storage temp-promote", 1)
	storageTempDeleteVerb          = verb(CommandModules, "storage temp-delete", 1)
	storageTempDeleteConfirmVerb   = verb(CommandModules, "storage temp-delete-confirm", 1)
	storageTempCleanupVerb         = verb(CommandModules, "storage temp-cleanup", 0)
	storageTempCleanupConfirmVerb  = verb(CommandModules, "storage temp-cleanup-confirm", 0)
	storageTempCleanupSettingsVerb = verb(CommandModules, "storage temp-cleanup-settings", 0)
	storageTempCleanupModeVerb     = verb(CommandModules, "storage temp-cleanup-mode", 0)
	storageTempToggleVerb          = verb(CommandModules, "storage temp-toggle", 1)
	storageTempDaysVerb            = valueVerb(CommandModules, "storage temp-days", 0)
	storageTempMaxVerb             = valueVerb(CommandModules, "storage temp-max", 0)
)

func (d *Dispatcher) handleStorage(ctx context.Context, r Route) (Result, error) {
	path := r.arg(0)
	switch r.Verb {
	case storageTempVerb:
		return d.storageTempPicker(ctx)
	case storageTempFileVerb:
		return d.storageTempFilePicker(ctx, path)
	case storageTempPromoteVerb:
		return d.storageTempPromote(ctx, path)
	case storageTempDeleteVerb:
		return d.storageTempDeleteConfirm(path), nil
	case storageTempDeleteConfirmVerb:
		return d.storageTempDelete(ctx, path)
	case storageTempCleanupVerb:
		return d.storageTempCleanup()
	case storageTempCleanupConfirmVerb:
		return d.storageTempCleanupConfirmed(ctx)
	case storageTempCleanupSettingsVerb:
		return d.storageTempCleanupSettings(ctx)
	case storageTempCleanupModeVerb:
		return d.storageTempCleanupModePicker(ctx)
	case storageTempToggleVerb:
		return d.storageTempToggle(ctx, path)
	case storageTempDaysVerb:
		return d.storageTempDays(ctx, r.Value)
	case storageTempMaxVerb:
		return d.storageTempMax(ctx, r.Value)
	case storageFilesVerb:
		return d.storageFilesPicker(ctx)
	case storageFileVerb:
		return d.storageFilePicker(ctx, path)
	case storageReadVerb:
		return d.storageRead(ctx, path)
	case storageDeleteVerb:
		return d.storageDeleteConfirm(path), nil
	case storageDeleteConfirmVerb:
		return d.storageDelete(ctx, path)
	case storageClearVerb:
		return d.storageClearConfirm(), nil
	case storageClearConfirmVerb:
		return d.storageClear(ctx)
	default:
		return d.storagePicker(ctx)
	}
}
