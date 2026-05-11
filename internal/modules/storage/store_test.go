package storage

import (
	"errors"
	"testing"
)

func TestLocalStoreSaveReadList(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}

	entry, err := store.Save("docs/inn.txt", "INN 1234567890", "Company INN", []string{"tax", "company"}, "text/plain")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if entry.Path != "docs/inn.txt" {
		t.Fatalf("entry path = %q, want docs/inn.txt", entry.Path)
	}

	readEntry, content, err := store.Read("docs/inn.txt", 0)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if readEntry.Title != "Company INN" {
		t.Fatalf("read title = %q, want Company INN", readEntry.Title)
	}
	if content != "INN 1234567890" {
		t.Fatalf("content = %q, want saved content", content)
	}

	entries, err := store.List("", "tax", 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "docs/inn.txt" {
		t.Fatalf("entries = %#v, want saved file", entries)
	}

	entries, err = store.List("", "1234567890", 10)
	if err != nil {
		t.Fatalf("List() content query error = %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "docs/inn.txt" {
		t.Fatalf("content query entries = %#v, want saved file", entries)
	}
}

func TestLocalStoreRejectsPathTraversal(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}
	for _, path := range []string{"../secret.txt", "/../secret.txt", ".matrixclaw/index.json"} {
		if _, err := store.Save(path, "x", "", nil, ""); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("Save(%q) error = %v, want ErrInvalidPath", path, err)
		}
	}
}

func TestLocalStoreUpdateMetadataAndDelete(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}
	if _, err := store.Save("docs/contract.txt", "contract", "", nil, "text/plain"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	entry, err := store.UpdateMetadata("docs/contract.txt", "Rent Contract", []string{"rent"}, "text/plain")
	if err != nil {
		t.Fatalf("UpdateMetadata() error = %v", err)
	}
	if entry.Title != "Rent Contract" || len(entry.Tags) != 1 || entry.Tags[0] != "rent" {
		t.Fatalf("entry = %#v, want updated metadata", entry)
	}
	deleted, err := store.Delete("docs/contract.txt")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deleted.Path != "docs/contract.txt" {
		t.Fatalf("deleted path = %q, want docs/contract.txt", deleted.Path)
	}
	entries, err := store.List("", "", 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want empty storage", entries)
	}
}

func TestLocalStoreTemporaryPromote(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}

	temp, err := store.SaveTemporary("scratch/note.txt", []byte("temporary content"), "Scratch", []string{"tmp"}, "text/plain")
	if err != nil {
		t.Fatalf("SaveTemporary() error = %v", err)
	}

	permanent, err := store.PromoteTemporary(temp.Path, "docs/note.txt")
	if err != nil {
		t.Fatalf("PromoteTemporary() error = %v", err)
	}
	if permanent.Path != "docs/note.txt" {
		t.Fatalf("PromoteTemporary().Path = %q, want docs/note.txt", permanent.Path)
	}
	if _, err := store.DeleteTemporary(temp.Path); err == nil {
		t.Fatal("DeleteTemporary(promoted temp) error = nil, want not found")
	}

	entry, content, err := store.readBytes(permanent.Path, 0)
	if err != nil {
		t.Fatalf("ReadBytes(promoted file) error = %v", err)
	}
	if entry.Path != permanent.Path || string(content) != "temporary content" {
		t.Fatalf("promoted read entry/content = %#v/%q, want promoted file content", entry, content)
	}
}
