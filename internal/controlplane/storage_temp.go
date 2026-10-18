package controlplane

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func (d *Dispatcher) storageTempPicker(ctx context.Context) (Result, error) {
	result, err := d.daemon.ListTemporaryStorageFiles(ctx, 50)
	if err != nil {
		return Result{}, err
	}
	items := make([]PickerItem, 0, len(result.Files)+2)
	for _, file := range result.Files {
		items = append(items, PickerItem{
			ID:      "temp:" + file.Path,
			Title:   storageTempTitle(file),
			Info:    storageTempInfo(file),
			Command: storageTempFileVerb.command(file.Path),
		})
	}
	if len(result.Files) == 0 {
		items = append(items, PickerItem{
			ID:       "empty",
			Title:    "No temporary files",
			Info:     formatFileCountSize(0, 0),
			Disabled: true,
		})
	}
	items = append(items, PickerItem{
		ID:      "settings",
		Title:   "Cleanup Settings",
		Info:    formatEnabled(result.Settings.AutoCleanup) + " · " + formatTempSettings(result.Settings),
		Command: storageTempCleanupSettingsVerb.command(),
		Role:    PickerItemRoleAction,
	})
	return Result{
		Screen: NewPickerData(PickerStorageTemp, "Temporary Files").Command(storageTempVerb.command()).Back(storageRootVerb.command()).Items(items...).Build(),
	}, nil
}

func (d *Dispatcher) storageTempFilePicker(ctx context.Context, tempPath string) (Result, error) {
	tempPath = strings.TrimSpace(tempPath)
	if tempPath == "" {
		return d.storageTempPicker(ctx)
	}
	return Result{
		Screen: NewPickerData(PickerStorageTempFile, "Temporary File").
			Back(storageTempVerb.command()).
			Row("promote", "Save", tempPath, storageTempPromoteVerb.command(tempPath)).
			Danger("delete", "Delete", "", storageTempDeleteVerb.command(tempPath)).
			Build(),
	}, nil
}

func (d *Dispatcher) storageTempPromote(ctx context.Context, tempPath string) (Result, error) {
	entry, err := d.daemon.PromoteTemporaryStorageFile(ctx, strings.TrimSpace(tempPath), "")
	if err != nil {
		return Result{}, err
	}
	return textResult("Saved to storage: " + entry.Path), nil
}

func (d *Dispatcher) storageTempDeleteConfirm(tempPath string) Result {
	tempPath = strings.TrimSpace(tempPath)
	if tempPath == "" {
		return textResult("Temporary file path is required.")
	}
	return Result{
		Screen: deleteConfirmData("Delete temporary file "+tempPath+"?", storageTempDeleteConfirmVerb.command(tempPath), storageTempFileVerb.command(tempPath)),
	}
}

func (d *Dispatcher) storageTempDelete(ctx context.Context, tempPath string) (Result, error) {
	entry, err := d.daemon.DeleteTemporaryStorageFile(ctx, strings.TrimSpace(tempPath))
	if err != nil {
		return Result{}, err
	}
	return textResult("Deleted temporary file: " + entry.Path), nil
}

func (d *Dispatcher) storageTempCleanup() (Result, error) {
	return Result{
		Screen: deleteConfirmData("Delete all temporary files?", storageTempCleanupConfirmVerb.command(), storageTempCleanupSettingsVerb.command()),
	}, nil
}

func (d *Dispatcher) storageTempCleanupConfirmed(ctx context.Context) (Result, error) {
	result, err := d.daemon.CleanupTemporaryStorageFiles(ctx)
	if err != nil {
		return Result{}, err
	}
	next, err := d.storageTempCleanupSettings(ctx)
	if err != nil {
		return Result{}, err
	}
	return next.withPickerMeta(fmt.Sprintf("Deleted %d files · freed %s", result.DeletedFiles, formatStorageSize(result.FreedBytes))), nil
}

func (d *Dispatcher) storageTempCleanupSettings(ctx context.Context) (Result, error) {
	result, err := d.daemon.ListTemporaryStorageFiles(ctx, 1)
	if err != nil {
		return Result{}, err
	}
	settings := result.Settings
	return Result{
		Screen: NewPickerData(PickerStorageCleanup, "Cleanup Settings").
			Back(storageTempVerb.command()).
			Row("toggle", "Auto Cleanup", formatEnabled(settings.AutoCleanup), storageTempCleanupModeVerb.command()).
			Row("days", "Retention", formatTempRetention(settings), storageTempDaysVerb.command()).
			Row("max", "Max Size", formatStorageGB(settings.MaxBytes), storageTempMaxVerb.command()).
			Action("cleanup", "Cleanup Now", formatTempCleanupImpact(settings), storageTempCleanupVerb.command()).
			Build(),
	}, nil
}

func (d *Dispatcher) storageTempCleanupModePicker(ctx context.Context) (Result, error) {
	result, err := d.daemon.ListTemporaryStorageFiles(ctx, 1)
	if err != nil {
		return Result{}, err
	}
	current := result.Settings.AutoCleanup
	return Result{
		Screen: NewPickerData(PickerStorageCleanup, "Auto Cleanup").
			Select(storageTempCleanupSettingsVerb.command()).
			Item(PickerItem{ID: "on", Title: "On", Selected: current, Command: storageTempToggleVerb.command("on")}).
			Item(PickerItem{ID: "off", Title: "Off", Selected: !current, Command: storageTempToggleVerb.command("off")}).
			Build(),
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
		result, err := d.daemon.ListTemporaryStorageFiles(ctx, 1)
		if err != nil {
			return Result{}, err
		}
		next = !result.Settings.AutoCleanup
	}
	if _, err := d.daemon.UpdateTemporaryStorageSettings(ctx, &next, 0, 0); err != nil {
		return Result{}, err
	}
	return d.storageTempCleanupSettings(ctx)
}

func (d *Dispatcher) storageTempDays(ctx context.Context, raw string) (Result, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Result{Screen: PromptData{
			Title:         "Delete Temporary Files After",
			Placeholder:   "7",
			Submit:        storageTempDaysVerb.command(),
			CancelCommand: storageTempCleanupSettingsVerb.command(),
		}}, nil
	}
	days, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || days <= 0 {
		return textResult("Days must be a positive number."), nil
	}
	if _, err := d.daemon.UpdateTemporaryStorageSettings(ctx, nil, days, 0); err != nil {
		return Result{}, err
	}
	return d.storageTempCleanupSettings(ctx)
}

func (d *Dispatcher) storageTempMax(ctx context.Context, raw string) (Result, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Result{Screen: PromptData{
			Title:         "Temporary Files Max Size",
			Placeholder:   "0.1",
			Submit:        storageTempMaxVerb.command(),
			CancelCommand: storageTempCleanupSettingsVerb.command(),
		}}, nil
	}
	gb, err := strconv.ParseFloat(raw, 64)
	if err != nil || gb <= 0 {
		return textResult("Max size must be a positive number of GB."), nil
	}
	if _, err := d.daemon.UpdateTemporaryStorageSettings(ctx, nil, 0, gb); err != nil {
		return Result{}, err
	}
	return d.storageTempCleanupSettings(ctx)
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
