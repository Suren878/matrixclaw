package controlplane

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/api"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func storageDaemon(t *testing.T) (*apiDaemon, *localstorage.LocalStore) {
	module, err := localstorage.New(localstorage.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return newAPIDaemon(t, api.Deps{Storage: module.Store()}), module.Store()
}

func TestStoredFilesPreviewAndDelete(t *testing.T) {
	daemon, store := storageDaemon(t)
	if _, err := store.SaveBytes("notes/a.md", []byte("# Notes"), "Notes", nil, "text/markdown"); err != nil {
		t.Fatal(err)
	}

	files := daemon.run("/modules storage files")
	if ids := pickerItemIDs(files); len(ids) != 2 || ids[0] != "file:notes/a.md" || ids[1] != "clear" {
		t.Fatalf("files = %v", ids)
	}
	file := daemon.run(screenOf[PickerData](t, files).Items[0].Command)
	if !isScreen[PickerData](file) || len(screenOf[PickerData](t, file).Items) != 2 {
		t.Fatalf("file = %+v", file)
	}
	if preview := daemon.run(screenOf[PickerData](t, file).Items[0].Command); screenOf[InfoData](t, preview).Text != "# Notes" {
		t.Fatalf("preview = %+v", preview)
	}
	asked := daemon.run(screenOf[PickerData](t, file).Items[1].Command)
	if !isScreen[ConfirmData](asked) {
		t.Fatalf("delete = %+v", asked)
	}
	daemon.run(screenOf[ConfirmData](t, asked).ConfirmCommand)
	if list, _ := store.List("", "", 10); len(list) != 0 {
		t.Fatalf("files after delete = %+v", list)
	}
}

func TestTemporaryFilesCleanupAfterConfirmation(t *testing.T) {
	daemon, store := storageDaemon(t)
	if _, err := store.SaveTemporary("calls/a.wav", []byte("RIFF"), "call", nil, "audio/wav"); err != nil {
		t.Fatal(err)
	}

	temp := daemon.run("/modules storage temp")
	if !isScreen[PickerData](temp) || len(screenOf[PickerData](t, temp).Items) == 0 {
		t.Fatalf("temp = %+v", temp)
	}
	asked := daemon.run("/modules storage temp-cleanup")
	if !isScreen[ConfirmData](asked) {
		t.Fatalf("cleanup = %+v", asked)
	}
	if result := daemon.run(screenOf[ConfirmData](t, asked).ConfirmCommand); result.Text() == "" && !isScreen[PickerData](result) {
		t.Fatalf("cleanup result = %+v", result)
	}
}
