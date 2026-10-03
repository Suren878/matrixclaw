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
	file := daemon.run(files.Picker.Items[0].Command)
	if file.Picker == nil || len(file.Picker.Items) != 2 {
		t.Fatalf("file = %+v", file)
	}
	if preview := daemon.run(file.Picker.Items[0].Command); preview.Info == nil || preview.Info.Text != "# Notes" {
		t.Fatalf("preview = %+v", preview)
	}
	asked := daemon.run(file.Picker.Items[1].Command)
	if asked.Confirm == nil {
		t.Fatalf("delete = %+v", asked)
	}
	daemon.run(asked.Confirm.ConfirmCommand)
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
	if temp.Picker == nil || len(temp.Picker.Items) == 0 {
		t.Fatalf("temp = %+v", temp)
	}
	asked := daemon.run("/modules storage temp-cleanup")
	if asked.Confirm == nil {
		t.Fatalf("cleanup = %+v", asked)
	}
	if result := daemon.run(asked.Confirm.ConfirmCommand); result.Text == "" && result.Picker == nil {
		t.Fatalf("cleanup result = %+v", result)
	}
}
