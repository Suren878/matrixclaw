package controlplane

import (
	"context"
	"strings"
)

func (d *Dispatcher) handleModules(ctx context.Context, args string) (Result, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return modulesPicker(), nil
	}
	switch strings.ToLower(fields[0]) {
	case "storage":
		return d.handleStorage(ctx, strings.TrimSpace(strings.TrimPrefix(args, fields[0])))
	default:
		return modulesPicker(), nil
	}
}

func modulesPicker() Result {
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerModules, "Modules").
			HideBack(true).
			Row("storage", "Storage", "Files", "/modules storage").
			CloseItem().
			Ptr(),
	}
}

func (d *Dispatcher) handleStorage(ctx context.Context, args string) (Result, error) {
	if d.storage == nil {
		return unsupportedRuntime("storage"), nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return d.storagePicker(ctx)
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return d.storagePicker(ctx)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
	switch strings.ToLower(fields[0]) {
	case "import":
		return d.storageImport(ctx, rest)
	case "temp":
		return d.storageTempPicker(ctx)
	case "temp-file":
		return d.storageTempFilePicker(ctx, rest)
	case "temp-promote":
		return d.storageTempPromote(ctx, rest)
	case "temp-delete":
		return d.storageTempDeleteConfirm(rest), nil
	case "temp-delete-confirm":
		return d.storageTempDelete(ctx, rest)
	case "temp-cleanup":
		return d.storageTempCleanup(ctx)
	case "temp-cleanup-confirm":
		return d.storageTempCleanupConfirmed(ctx)
	case "temp-cleanup-mode":
		return d.storageTempCleanupModePicker(ctx)
	case "temp-toggle":
		return d.storageTempToggle(ctx, rest)
	case "temp-days":
		return d.storageTempDays(ctx, rest)
	case "temp-max":
		return d.storageTempMax(ctx, rest)
	case "files":
		return d.storageFilesPicker(ctx)
	case "file":
		return d.storageFilePicker(ctx, rest)
	case "read":
		return d.storageRead(ctx, rest)
	case "delete":
		return d.storageDeleteConfirm(rest), nil
	case "delete-confirm":
		return d.storageDelete(ctx, rest)
	default:
		return d.storagePicker(ctx)
	}
}
