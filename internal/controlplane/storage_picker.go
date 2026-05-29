package controlplane

import (
	"context"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func (d *Dispatcher) storagePicker(ctx context.Context) (Result, error) {
	tempInfo := ""
	if temp, err := d.storage.ListTemporaryStorageFiles(ctx, 1); err == nil {
		tempInfo = formatFileCountSize(temp.Settings.TotalFiles, temp.Settings.TotalBytes)
	}
	storedInfo := ""
	if stored, err := d.storage.ListStorageFiles(ctx, localstorage.ListFilter{Limit: 200}); err == nil {
		storedInfo = formatStoredFilesInfo(stored.Files)
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerStorage, "Storage").
			Back(modulesCommand()).
			Row("files", "Stored Files", storedInfo, storageFilesCommand()).
			Row("temp", "Temporary Files", tempInfo, storageTempCommand()).
			Ptr(),
	}, nil
}
