package controlplane

import (
	"context"
	"fmt"
	"strings"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func (d *Dispatcher) storageFilesPicker(ctx context.Context) (Result, error) {
	list, err := d.storage.ListStorageFiles(ctx, localstorage.ListFilter{Limit: 50})
	if err != nil {
		return Result{}, err
	}
	items := make([]PickerItem, 0, len(list.Files)+2)
	for _, file := range list.Files {
		items = append(items, PickerItem{
			ID:    "file:" + file.Path,
			Title: storageFileTitle(file),
			Info:  storageFileInfo(file),
		})
	}
	if len(items) == 0 {
		items = append(items, PickerItem{
			ID:       "empty",
			Title:    "No stored files yet",
			Info:     "Ask the assistant to save one.",
			Disabled: true,
		})
	} else {
		items = append(items, PickerItem{
			ID:    "clear",
			Title: "Clear All",
			Info:  "Delete all files",
			Role:  PickerItemRoleDanger,
		})
	}
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerStorageFiles, "Stored Files").Back(storageCommand()).Items(items...).Ptr(),
	}, nil
}

func (d *Dispatcher) storageFilePicker(ctx context.Context, storagePath string) (Result, error) {
	storagePath = strings.TrimSpace(storagePath)
	if storagePath == "" {
		return d.storageFilesPicker(ctx)
	}
	read, err := d.storage.ReadStorageFile(ctx, storagePath)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerStorageFile, "Storage File").
			Context(read.File.Path).
			Back(storageFilesCommand()).
			Row("read", "Preview", storageFileTitle(read.File)).
			Danger("delete", "Delete", "").
			Ptr(),
	}, nil
}

func (d *Dispatcher) storageRead(ctx context.Context, storagePath string) (Result, error) {
	read, err := d.storage.ReadStorageFile(ctx, strings.TrimSpace(storagePath))
	if err != nil {
		return Result{}, err
	}
	content := strings.TrimSpace(read.Content)
	if len(content) > 4000 {
		content = content[:4000] + "\n..."
	}
	return Result{
		Handled: true,
		Info: &InfoData{
			Title:        storageFileTitle(read.File),
			Text:         content,
			CloseCommand: storageFileCommand(read.File.Path),
		},
	}, nil
}

func (d *Dispatcher) storageDeleteConfirm(storagePath string) Result {
	storagePath = strings.TrimSpace(storagePath)
	if storagePath == "" {
		return Result{Handled: true, Text: "Storage path is required."}
	}
	return Result{
		Handled: true,
		Confirm: deleteConfirmData("Delete "+storagePath+"?", storageDeleteConfirmCommand(storagePath), storageFileCommand(storagePath)),
	}
}

func (d *Dispatcher) storageDelete(ctx context.Context, storagePath string) (Result, error) {
	if _, err := d.storage.DeleteStorageFile(ctx, strings.TrimSpace(storagePath)); err != nil {
		return Result{}, err
	}
	return d.storageFilesPicker(ctx)
}

func (d *Dispatcher) storageClearConfirm() Result {
	return Result{
		Handled: true,
		Confirm: deleteConfirmData("Delete all files?", storageClearConfirmCommand(), storageFilesCommand()),
	}
}

func (d *Dispatcher) storageClear(ctx context.Context) (Result, error) {
	var deleted int
	var freed int64
	for {
		list, err := d.storage.ListStorageFiles(ctx, localstorage.ListFilter{Limit: 200})
		if err != nil {
			return Result{}, err
		}
		if len(list.Files) == 0 {
			break
		}
		for _, file := range list.Files {
			entry, err := d.storage.DeleteStorageFile(ctx, file.Path)
			if err != nil {
				return Result{}, err
			}
			deleted++
			freed += entry.Size
		}
		if len(list.Files) < 200 {
			break
		}
	}
	result, err := d.storageFilesPicker(ctx)
	if err != nil {
		return Result{}, err
	}
	if result.Picker != nil {
		result.Picker.Meta = fmt.Sprintf("Deleted %d files · freed %s", deleted, formatStorageSize(freed))
	}
	return result, nil
}

func storageFileTitle(file localstorage.Entry) string {
	if strings.TrimSpace(file.Title) != "" {
		return strings.TrimSpace(file.Title)
	}
	return strings.TrimSpace(file.Path)
}

func storageFileInfo(file localstorage.Entry) string {
	parts := []string{}
	if strings.TrimSpace(file.Title) != "" && strings.TrimSpace(file.Path) != "" {
		parts = append(parts, file.Path)
	}
	if file.Size > 0 {
		parts = append(parts, fmt.Sprintf("%d bytes", file.Size))
	}
	if len(file.Tags) > 0 {
		parts = append(parts, strings.Join(file.Tags, ", "))
	}
	return strings.Join(parts, " · ")
}
