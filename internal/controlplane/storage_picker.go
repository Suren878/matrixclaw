package controlplane

import "context"

func (d *Dispatcher) storagePicker(ctx context.Context) (Result, error) {
	tempInfo := ""
	if temp, err := d.storage.ListTemporaryStorageFiles(ctx, 1); err == nil {
		tempInfo = formatEnabled(temp.Settings.AutoCleanup) + " · " + formatTempSettings(temp.Settings)
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerStorage, "Storage").
			Back(modulesCommand()).
			Close("").
			Row("temp", "Temporary Files", tempInfo).
			Row("files", "Stored Files", "").
			Row("import", "Import from Path", "").
			Ptr(),
	}, nil
}
