package storage

import (
	"encoding/json"
	"testing"
)

func TestAPIContractJSONFields(t *testing.T) {
	autoCleanup := true

	assertJSONFields(t, FileSaveRequest{Path: "docs/a.txt", ContentBase64: "cGxhaW4=", Title: "A", Tags: []string{"x"}, MIMEType: "text/plain"}, "path", "content_base64", "title", "tags", "mime_type")
	assertJSONFields(t, FileResponse{File: Entry{Path: "docs/a.txt"}}, "file")
	assertJSONFields(t, TempFileResponse{File: TempEntry{Path: "tmp/a.txt"}}, "file")
	assertJSONFields(t, TempPromoteRequest{DestPath: "docs/a.txt"}, "dest_path")
	assertJSONFields(t, TempSettingsUpdateRequest{AutoCleanup: &autoCleanup, TTLDays: 7, MaxGB: 1.5}, "auto_cleanup", "ttl_days", "max_gb")
	assertJSONFields(t, TempSettingsResponse{Settings: TempSettings{AutoCleanup: true}}, "settings")
	assertJSONFields(t, CleanupResponse{Cleanup: CleanupResult{DeletedFiles: 1}}, "cleanup")
}

func TestTempSettingsUpdateRequestOmitsUnsetOptionalFields(t *testing.T) {
	data, fields := jsonFields(t, TempSettingsUpdateRequest{TTLDays: 7, MaxGB: 0.5})
	for _, field := range []string{"auto_cleanup"} {
		if _, ok := fields[field]; ok {
			t.Fatalf("json field %q is present in %s", field, data)
		}
	}
}

func TestFileSaveRequestHelpers(t *testing.T) {
	request := NewFileSaveRequest(" docs/a.txt ", []byte("hello"), " A ", []string{"x"}, " text/plain ")
	if request.Path != "docs/a.txt" {
		t.Fatalf("Path = %q, want trimmed path", request.Path)
	}
	if request.Title != "A" {
		t.Fatalf("Title = %q, want trimmed title", request.Title)
	}
	if request.MIMEType != "text/plain" {
		t.Fatalf("MIMEType = %q, want trimmed MIME type", request.MIMEType)
	}
	if request.ContentBase64 != "aGVsbG8=" {
		t.Fatalf("ContentBase64 = %q, want base64 encoded content", request.ContentBase64)
	}

	content, err := (FileSaveRequest{
		ContentBase64: " aGVsbG8= ",
	}).ContentBytes()
	if err != nil {
		t.Fatalf("ContentBytes() error = %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("ContentBytes() = %q, want decoded base64 content", content)
	}
}

func assertJSONFields(t *testing.T, value any, names ...string) {
	t.Helper()

	data, fields := jsonFields(t, value)
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			t.Fatalf("json field %q is missing from %T: %s", name, value, data)
		}
	}
}

func jsonFields(t *testing.T, value any) (string, map[string]json.RawMessage) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal json fields: %v", err)
	}
	return string(data), fields
}
