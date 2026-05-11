package controlplane

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func (d *Dispatcher) storageTempPicker(ctx context.Context) (Result, error) {
	result, err := d.storage.ListTemporaryStorageFiles(ctx, 50)
	if err != nil {
		return Result{}, err
	}
	items := make([]PickerItem, 0, len(result.Files)+5)
	for _, file := range result.Files {
		items = append(items, PickerItem{
			ID:    "temp:" + file.Path,
			Title: storageTempTitle(file),
			Info:  storageTempInfo(file),
		})
	}
	items = append(items,
		PickerItem{ID: "toggle", Title: "Auto Cleanup", Info: formatEnabled(result.Settings.AutoCleanup)},
		PickerItem{ID: "days", Title: "Retention", Info: formatTempRetention(result.Settings)},
		PickerItem{ID: "max", Title: "Max Size", Info: formatStorageGB(result.Settings.MaxBytes)},
		PickerItem{ID: "cleanup", Title: "Cleanup", Info: formatTempCleanupImpact(result.Settings), Role: PickerItemRoleAction},
	)
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerStorageTemp, "Temporary Files").Back("/modules storage").Items(items...).Ptr(),
	}, nil
}

func (d *Dispatcher) storageTempFilePicker(ctx context.Context, tempPath string) (Result, error) {
	tempPath = strings.TrimSpace(tempPath)
	if tempPath == "" {
		return d.storageTempPicker(ctx)
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerStorageTempFile, "Temporary File").
			Context(tempPath).
			Back("/modules storage temp").
			Row("promote", "Save", tempPath).
			Danger("delete", "Delete", "").
			Ptr(),
	}, nil
}

func (d *Dispatcher) storageTempPromote(ctx context.Context, tempPath string) (Result, error) {
	entry, err := d.storage.PromoteTemporaryStorageFile(ctx, strings.TrimSpace(tempPath), "")
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "Saved to storage: " + entry.Path}, nil
}

func (d *Dispatcher) storageTempDeleteConfirm(tempPath string) Result {
	tempPath = strings.TrimSpace(tempPath)
	if tempPath == "" {
		return Result{Handled: true, Text: "Temporary file path is required."}
	}
	return Result{
		Handled: true,
		Confirm: deleteConfirmData("Delete temporary file "+tempPath+"?", "/modules storage temp-delete-confirm "+tempPath, "/modules storage temp-file "+tempPath),
	}
}

func (d *Dispatcher) storageTempDelete(ctx context.Context, tempPath string) (Result, error) {
	entry, err := d.storage.DeleteTemporaryStorageFile(ctx, strings.TrimSpace(tempPath))
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "Deleted temporary file: " + entry.Path}, nil
}

func (d *Dispatcher) storageTempCleanup(ctx context.Context) (Result, error) {
	return Result{
		Handled: true,
		Confirm: deleteConfirmData("Delete all temporary files?", "/modules storage temp-cleanup-confirm", "/modules storage temp"),
	}, nil
}

func (d *Dispatcher) storageTempCleanupConfirmed(ctx context.Context) (Result, error) {
	result, err := d.storage.CleanupTemporaryStorageFiles(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: fmt.Sprintf("Temporary cleanup: deleted %d files, freed %s.", result.DeletedFiles, formatStorageSize(result.FreedBytes))}, nil
}

func (d *Dispatcher) storageTempCleanupModePicker(ctx context.Context) (Result, error) {
	result, err := d.storage.ListTemporaryStorageFiles(ctx, 1)
	if err != nil {
		return Result{}, err
	}
	current := result.Settings.AutoCleanup
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerStorageCleanup, "Auto Cleanup").
			Back("/modules storage temp").
			Item(PickerItem{ID: "on", Title: "On", Selected: current}).
			Item(PickerItem{ID: "off", Title: "Off", Selected: !current}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) storageTempToggle(ctx context.Context, value string) (Result, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	var next bool
	switch value {
	case "on", "enabled", "enable":
		next = true
	case "off", "disabled", "disable":
		next = false
	default:
		result, err := d.storage.ListTemporaryStorageFiles(ctx, 1)
		if err != nil {
			return Result{}, err
		}
		next = !result.Settings.AutoCleanup
	}
	if _, err := d.storage.UpdateTemporaryStorageSettings(ctx, &next, 0, 0); err != nil {
		return Result{}, err
	}
	return d.storageTempPicker(ctx)
}

func (d *Dispatcher) storageTempDays(ctx context.Context, raw string) (Result, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Result{Handled: true, Prompt: &PromptData{
			Title:               "Delete Temporary Files After",
			Placeholder:         "7",
			SubmitCommandPrefix: "/modules storage temp-days ",
			CancelCommand:       "/modules storage temp",
		}}, nil
	}
	days, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || days <= 0 {
		return Result{Handled: true, Text: "Days must be a positive number."}, nil
	}
	if _, err := d.storage.UpdateTemporaryStorageSettings(ctx, nil, days, 0); err != nil {
		return Result{}, err
	}
	return d.storageTempPicker(ctx)
}

func (d *Dispatcher) storageTempMax(ctx context.Context, raw string) (Result, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Result{Handled: true, Prompt: &PromptData{
			Title:               "Temporary Files Max Size",
			Placeholder:         "0.1",
			SubmitCommandPrefix: "/modules storage temp-max ",
			CancelCommand:       "/modules storage temp",
		}}, nil
	}
	gb, err := strconv.ParseFloat(raw, 64)
	if err != nil || gb <= 0 {
		return Result{Handled: true, Text: "Max size must be a positive number of GB."}, nil
	}
	if _, err := d.storage.UpdateTemporaryStorageSettings(ctx, nil, 0, gb); err != nil {
		return Result{}, err
	}
	return d.storageTempPicker(ctx)
}

func storageTempTitle(file localstorage.TempEntry) string {
	if strings.TrimSpace(file.Title) != "" {
		return strings.TrimSpace(file.Title)
	}
	return strings.TrimSpace(file.Path)
}

func storageTempInfo(file localstorage.TempEntry) string {
	parts := []string{file.Path}
	if file.Size > 0 {
		parts = append(parts, fmt.Sprintf("%d bytes", file.Size))
	}
	if !file.ExpiresAt.IsZero() {
		parts = append(parts, "expires "+file.ExpiresAt.Local().Format("2006-01-02 15:04"))
	}
	return strings.Join(parts, " · ")
}
