package controlplane

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func (d *Dispatcher) storageImport(ctx context.Context, localPath string) (Result, error) {
	localPath = strings.Trim(strings.TrimSpace(localPath), `"`)
	if localPath == "" {
		return Result{
			Handled: true,
			Prompt: &PromptData{
				Title:               "Local File Path",
				Placeholder:         "/absolute/path/to/file.txt",
				SubmitCommandPrefix: "/modules storage import ",
				CancelCommand:       "/modules storage",
			},
		}, nil
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return Result{}, err
	}
	if info.IsDir() {
		return Result{Handled: true, Text: "Cannot import a directory."}, nil
	}
	content, err := os.ReadFile(localPath)
	if err != nil {
		return Result{}, err
	}
	mimeType := http.DetectContentType(content)
	name := filepath.Base(localPath)
	entry, err := d.storage.SaveStorageFile(ctx, "imports/"+name, content, name, []string{"import"}, mimeType)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "Imported to storage: " + entry.Path}, nil
}

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
			ID:      "empty",
			Title:   "No stored files yet",
			Info:    "Import a file or ask the assistant to save one.",
			Command: "/modules storage import",
		})
	}
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerStorageFiles, "Stored Files").Back("/modules storage").Items(items...).Ptr(),
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
			Back("/modules storage files").
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
		Text:    fmt.Sprintf("%s\n\n%s", storageFileTitle(read.File), content),
	}, nil
}

func (d *Dispatcher) storageDeleteConfirm(storagePath string) Result {
	storagePath = strings.TrimSpace(storagePath)
	if storagePath == "" {
		return Result{Handled: true, Text: "Storage path is required."}
	}
	return Result{
		Handled: true,
		Confirm: deleteConfirmData("Delete "+storagePath+"?", "/modules storage delete-confirm "+storagePath, "/modules storage file "+storagePath),
	}
}

func (d *Dispatcher) storageDelete(ctx context.Context, storagePath string) (Result, error) {
	entry, err := d.storage.DeleteStorageFile(ctx, strings.TrimSpace(storagePath))
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "Deleted storage file: " + entry.Path}, nil
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
