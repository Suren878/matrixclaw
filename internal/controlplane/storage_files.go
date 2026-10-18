package controlplane

import (
	"context"
	"fmt"
	"strings"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func (d *Dispatcher) storageFilesPicker(ctx context.Context) (Result, error) {
	list, err := d.daemon.ListStorageFiles(ctx, localstorage.ListFilter{Limit: 50})
	if err != nil {
		return Result{}, err
	}
	items := make([]PickerItem, 0, len(list.Files)+2)
	for _, file := range list.Files {
		items = append(items, PickerItem{
			ID:      "file:" + file.Path,
			Title:   storageFileTitle(file),
			Info:    storageFileInfo(file),
			Command: storageFileVerb.command(file.Path),
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
			ID:      "clear",
			Title:   "Clear All",
			Info:    "Delete all files",
			Command: storageClearVerb.command(),
			Role:    PickerItemRoleDanger,
		})
	}
	return Result{
		Screen: NewPickerData(PickerStorageFiles, "Stored Files").Command(storageFilesVerb.command()).Back(storageRootVerb.command()).Items(items...).Build(),
	}, nil
}

func (d *Dispatcher) storageFilePicker(ctx context.Context, storagePath string) (Result, error) {
	storagePath = strings.TrimSpace(storagePath)
	if storagePath == "" {
		return d.storageFilesPicker(ctx)
	}
	read, err := d.daemon.ReadStorageFile(ctx, storagePath)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Screen: NewPickerData(PickerStorageFile, "Storage File").
			Back(storageFilesVerb.command()).
			Row("read", "Preview", storageFileTitle(read.File), storageReadVerb.command(read.File.Path)).
			Danger("delete", "Delete", "", storageDeleteVerb.command(read.File.Path)).
			Build(),
	}, nil
}

func (d *Dispatcher) storageRead(ctx context.Context, storagePath string) (Result, error) {
	read, err := d.daemon.ReadStorageFile(ctx, strings.TrimSpace(storagePath))
	if err != nil {
		return Result{}, err
	}
	content := strings.TrimSpace(read.Content)
	if len(content) > 4000 {
		content = content[:4000] + "\n..."
	}
	return Result{
		Screen: InfoData{
			Title: storageFileTitle(read.File),
			Text:  content,
		},
	}, nil
}

func (d *Dispatcher) storageDeleteConfirm(storagePath string) Result {
	storagePath = strings.TrimSpace(storagePath)
	if storagePath == "" {
		return textResult("Storage path is required.")
	}
	return Result{
		Screen: deleteConfirmData("Delete "+storagePath+"?", storageDeleteConfirmVerb.command(storagePath), storageFileVerb.command(storagePath)),
	}
}

func (d *Dispatcher) storageDelete(ctx context.Context, storagePath string) (Result, error) {
	if _, err := d.daemon.DeleteStorageFile(ctx, strings.TrimSpace(storagePath)); err != nil {
		return Result{}, err
	}
	return d.storageFilesPicker(ctx)
}

func (d *Dispatcher) storageClearConfirm() Result {
	return Result{
		Screen: deleteConfirmData("Delete all files?", storageClearConfirmVerb.command(), storageFilesVerb.command()),
	}
}

func (d *Dispatcher) storageClear(ctx context.Context) (Result, error) {
	var deleted int
	var freed int64
	for {
		list, err := d.daemon.ListStorageFiles(ctx, localstorage.ListFilter{Limit: 200})
		if err != nil {
			return Result{}, err
		}
		if len(list.Files) == 0 {
			break
		}
		for _, file := range list.Files {
			entry, err := d.daemon.DeleteStorageFile(ctx, file.Path)
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
	return result.withPickerMeta(fmt.Sprintf("Deleted %d files · freed %s", deleted, formatStorageSize(freed))), nil
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
