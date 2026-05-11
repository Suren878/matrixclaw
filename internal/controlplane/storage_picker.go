package controlplane

import (
	"context"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func (d *Dispatcher) storagePicker(ctx context.Context) (Result, error) {
	list, err := d.storage.ListStorageFiles(ctx, localstorage.ListFilter{Limit: 1})
	if err != nil {
		return Result{}, err
	}
	tempInfo := "Temporary"
	if temp, err := d.storage.ListTemporaryStorageFiles(ctx, 1); err == nil {
		tempInfo = formatEnabled(temp.Settings.AutoCleanup) + " · " + formatTempSettings(temp.Settings)
	}
	info := "No files"
	if len(list.Files) > 0 {
		info = "Saved files"
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerStorage, "Storage").
			Back("/modules").
			Close("").
			Row("temp", "Temporary Files", tempInfo).
			Row("import", "Import File", "").
			Row("files", "Files", info).
			Ptr(),
	}, nil
}
