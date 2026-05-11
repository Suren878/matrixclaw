package storage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestStorageTools(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 4096)
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}

	saveArgs, _ := json.Marshal(map[string]any{
		"path":      "contracts/rent.txt",
		"content":   "Rent contract",
		"title":     "Rent",
		"tags":      []string{"contract"},
		"mime_type": "text/plain",
	})
	saveResult, err := NewSaveTool(store).Execute(context.Background(), tools.Call{Args: saveArgs})
	if err != nil {
		t.Fatalf("save Execute() error = %v", err)
	}
	if saveResult.IsError || !strings.Contains(saveResult.Content, "Saved to storage") {
		t.Fatalf("save result = %#v", saveResult)
	}

	listArgs, _ := json.Marshal(map[string]any{"query": "contract"})
	listResult, err := NewListTool(store).Execute(context.Background(), tools.Call{Args: listArgs})
	if err != nil {
		t.Fatalf("list Execute() error = %v", err)
	}
	if !strings.Contains(listResult.Content, "contracts/rent.txt") {
		t.Fatalf("list content = %q, want saved file", listResult.Content)
	}

	readArgs, _ := json.Marshal(map[string]any{"path": "contracts/rent.txt"})
	readResult, err := NewReadTool(store).Execute(context.Background(), tools.Call{Args: readArgs})
	if err != nil {
		t.Fatalf("read Execute() error = %v", err)
	}
	if !strings.Contains(readResult.Content, "Rent contract") {
		t.Fatalf("read content = %q, want saved content", readResult.Content)
	}

	updateArgs, _ := json.Marshal(map[string]any{
		"path":  "contracts/rent.txt",
		"title": "Updated Rent",
		"tags":  []string{"lease"},
	})
	updateResult, err := NewUpdateMetadataTool(store).Execute(context.Background(), tools.Call{Args: updateArgs})
	if err != nil {
		t.Fatalf("update Execute() error = %v", err)
	}
	if updateResult.IsError || !strings.Contains(updateResult.Content, "metadata updated") {
		t.Fatalf("update result = %#v", updateResult)
	}

	if _, err := store.SaveTemporary("uploads/photo.png", []byte("png-bytes"), "photo.png", []string{"upload"}, "image/png"); err != nil {
		t.Fatalf("SaveTemporary() error = %v", err)
	}
	promoteArgs, _ := json.Marshal(map[string]any{
		"temp_path": "uploads/photo.png",
		"dest_path": "documents/photo.png",
	})
	promoteResult, err := NewSaveTemporaryTool(store).Execute(context.Background(), tools.Call{Args: promoteArgs})
	if err != nil {
		t.Fatalf("promote Execute() error = %v", err)
	}
	if promoteResult.IsError || !strings.Contains(promoteResult.Content, "documents/photo.png") {
		t.Fatalf("promote result = %#v", promoteResult)
	}
	if _, _, err := store.ReadTemporaryBytes("uploads/photo.png"); err != nil {
		t.Fatalf("temporary upload should remain readable after storage_save_temp: %v", err)
	}

	deleteArgs, _ := json.Marshal(map[string]any{"path": "contracts/rent.txt"})
	pending, err := NewDeleteTool(store).Execute(context.Background(), tools.Call{Args: deleteArgs})
	if err != nil {
		t.Fatalf("delete pending Execute() error = %v", err)
	}
	if pending.Approval == nil {
		t.Fatalf("delete should require approval: %#v", pending)
	}
	deleteResult, err := NewDeleteTool(store).Execute(context.Background(), tools.Call{Args: deleteArgs, Approved: true})
	if err != nil {
		t.Fatalf("delete Execute() error = %v", err)
	}
	if deleteResult.IsError || !strings.Contains(deleteResult.Content, "Deleted from storage") {
		t.Fatalf("delete result = %#v", deleteResult)
	}
}

func TestReadToolUsesNarrowStoreInterface(t *testing.T) {
	store := &fakeReadStore{
		entry:   Entry{Path: "docs/fake.txt", MIMEType: "text/plain"},
		content: "from fake store\n",
	}
	args, _ := json.Marshal(map[string]any{"path": "docs/fake.txt"})

	result, err := NewReadTool(store).Execute(context.Background(), tools.Call{Args: args})
	if err != nil {
		t.Fatalf("read Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("read result is error: %#v", result)
	}
	if !strings.Contains(result.Content, "from fake store") {
		t.Fatalf("read content = %q, want fake store content", result.Content)
	}
	if store.readPath != "docs/fake.txt" {
		t.Fatalf("read path = %q, want docs/fake.txt", store.readPath)
	}
}

type fakeReadStore struct {
	entry    Entry
	content  string
	readPath string
}

func (s *fakeReadStore) Read(rawPath string, maxBytes int64) (Entry, string, error) {
	s.readPath = rawPath
	return s.entry, s.content, nil
}
