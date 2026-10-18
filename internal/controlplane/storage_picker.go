package controlplane

import (
	"context"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func (d *Dispatcher) storagePicker(ctx context.Context) (Result, error) {
	tempInfo := ""
	if temp, err := d.daemon.ListTemporaryStorageFiles(ctx, 1); err == nil {
		tempInfo = formatFileCountSize(temp.Settings.TotalFiles, temp.Settings.TotalBytes)
	}
	storedInfo := ""
	if stored, err := d.daemon.ListStorageFiles(ctx, localstorage.ListFilter{Limit: 200}); err == nil {
		storedInfo = formatStoredFilesInfo(stored.Files)
	}
	return Result{
		Screen: NewPickerData(PickerStorage, "Storage").
			Back(modulesRoot.command()).
			Row("files", "Stored Files", storedInfo, storageFilesVerb.command()).
			Row("temp", "Temporary Files", tempInfo, storageTempVerb.command()).
			Build(),
	}, nil
}
