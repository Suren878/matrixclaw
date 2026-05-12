package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

type saveStore interface {
	Save(rawPath string, content string, title string, tags []string, mimeType string) (Entry, error)
}

type readStore interface {
	Read(rawPath string, maxBytes int64) (Entry, string, error)
}

type listStore interface {
	List(prefix string, query string, limit int) ([]Entry, error)
}

type updateMetadataStore interface {
	UpdateMetadata(rawPath string, title string, tags []string, mimeType string) (Entry, error)
}

type deleteStore interface {
	Delete(rawPath string) (Entry, error)
}

type saveTemporaryStore interface {
	CopyTemporary(rawPath string, destPath string) (Entry, error)
}

type saveTool struct {
	store saveStore
}

type readTool struct {
	store readStore
}

type listTool struct {
	store listStore
}

type updateMetadataTool struct {
	store updateMetadataStore
}

type deleteTool struct {
	store deleteStore
}

type saveTemporaryTool struct {
	store saveTemporaryStore
}

func NewSaveTool(store saveStore) tools.Executor {
	return &saveTool{store: store}
}

func NewReadTool(store readStore) tools.Executor {
	return &readTool{store: store}
}

func NewListTool(store listStore) tools.Executor {
	return &listTool{store: store}
}

func NewUpdateMetadataTool(store updateMetadataStore) tools.Executor {
	return &updateMetadataTool{store: store}
}

func NewDeleteTool(store deleteStore) tools.Executor {
	return &deleteTool{store: store}
}

func NewSaveTemporaryTool(store saveTemporaryStore) tools.Executor {
	return &saveTemporaryTool{store: store}
}

func (t *saveTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          "storage_save",
		Name:        "Storage Save",
		Description: "Save text or document content into matrixclaw local storage. Use for user documents, generated notes, contracts, IDs, and reusable project files.",
		Risk:        tools.RiskSafe,
		Namespace:   "module.storage",
		Category:    tools.CategoryStorage,
		Profiles:    []tools.Profile{tools.ProfileStorage},
		OutputKind:  tools.OutputStorageEntry,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Relative storage path, for example docs/contract.txt"},
    "content": {"type": "string"},
    "title": {"type": "string"},
    "tags": {"type": "array", "items": {"type": "string"}},
    "mime_type": {"type": "string"}
  },
  "required": ["path", "content"],
  "additionalProperties": false
}`),
	}
}

func (t *saveTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.store == nil {
		return tools.Result{Content: "Local storage is not configured.", IsError: true}, nil
	}
	var input struct {
		Path     string   `json:"path"`
		Content  string   `json:"content"`
		Title    string   `json:"title"`
		Tags     []string `json:"tags"`
		MIMEType string   `json:"mime_type"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid storage_save arguments.", IsError: true}, nil
	}
	entry, err := t.store.Save(input.Path, input.Content, input.Title, input.Tags, input.MIMEType)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Storage save failed: %s", err), IsError: true}, nil
	}
	return tools.Result{
		Content:  fmt.Sprintf("Saved to storage: %s", entry.Path),
		Metadata: entry,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func (t *readTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          "storage_read",
		Name:        "Storage Read",
		Description: "Read a text file from matrixclaw local storage by relative storage path.",
		Risk:        tools.RiskSafe,
		Namespace:   "module.storage",
		Category:    tools.CategoryStorage,
		Profiles:    []tools.Profile{tools.ProfileStorage},
		OutputKind:  tools.OutputFileContent,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "max_bytes": {"type": "integer", "minimum": 1}
  },
  "required": ["path"],
  "additionalProperties": false
}`),
	}
}

func (t *readTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.store == nil {
		return tools.Result{Content: "Local storage is not configured.", IsError: true}, nil
	}
	var input struct {
		Path     string `json:"path"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid storage_read arguments.", IsError: true}, nil
	}
	entry, content, err := t.store.Read(input.Path, input.MaxBytes)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Storage read failed: %s", err), IsError: true}, nil
	}
	body := strings.TrimRight(content, "\n")
	return tools.Result{
		Content:  fmt.Sprintf("<storage path=%q>\n%s\n</storage>", entry.Path, body),
		Metadata: entry,
		MIMEType: entry.MIMEType,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func (t *listTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          "storage_list",
		Name:        "Storage List",
		Description: "List matrixclaw local storage files. Filter by prefix or query over path, title, and tags.",
		Risk:        tools.RiskSafe,
		Namespace:   "module.storage",
		Category:    tools.CategoryStorage,
		Profiles:    []tools.Profile{tools.ProfileStorage},
		OutputKind:  tools.OutputStorageList,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "prefix": {"type": "string"},
    "query": {"type": "string"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200}
  },
  "additionalProperties": false
}`),
	}
}

func (t *listTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.store == nil {
		return tools.Result{Content: "Local storage is not configured.", IsError: true}, nil
	}
	var input struct {
		Prefix string `json:"prefix"`
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &input); err != nil {
			return tools.Result{Content: "Invalid storage_list arguments.", IsError: true}, nil
		}
	}
	entries, err := t.store.List(input.Prefix, input.Query, input.Limit)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Storage list failed: %s", err), IsError: true}, nil
	}
	if len(entries) == 0 {
		return tools.Result{Content: "Storage is empty.", Metadata: entries, Status: tools.ResultStatusSuccess}, nil
	}
	var out strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&out, "- %s", entry.Path)
		if entry.Title != "" {
			fmt.Fprintf(&out, " — %s", entry.Title)
		}
		if len(entry.Tags) > 0 {
			fmt.Fprintf(&out, " [%s]", strings.Join(entry.Tags, ", "))
		}
		fmt.Fprintf(&out, " (%d bytes)\n", entry.Size)
	}
	return tools.Result{
		Content:  strings.TrimRight(out.String(), "\n"),
		Metadata: entries,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func (t *updateMetadataTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          "storage_update_metadata",
		Name:        "Storage Update Metadata",
		Description: "Update title, tags, and MIME type for a file in matrixclaw local storage.",
		Risk:        tools.RiskSafe,
		Namespace:   "module.storage",
		Category:    tools.CategoryStorage,
		Profiles:    []tools.Profile{tools.ProfileStorage},
		OutputKind:  tools.OutputStorageEntry,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "title": {"type": "string"},
    "tags": {"type": "array", "items": {"type": "string"}},
    "mime_type": {"type": "string"}
  },
  "required": ["path"],
  "additionalProperties": false
}`),
	}
}

func (t *updateMetadataTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.store == nil {
		return tools.Result{Content: "Local storage is not configured.", IsError: true}, nil
	}
	var input struct {
		Path     string   `json:"path"`
		Title    string   `json:"title"`
		Tags     []string `json:"tags"`
		MIMEType string   `json:"mime_type"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid storage_update_metadata arguments.", IsError: true}, nil
	}
	entry, err := t.store.UpdateMetadata(input.Path, input.Title, input.Tags, input.MIMEType)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Storage metadata update failed: %s", err), IsError: true}, nil
	}
	return tools.Result{
		Content:  fmt.Sprintf("Storage metadata updated: %s", entry.Path),
		Metadata: entry,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func (t *deleteTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          "storage_delete",
		Name:        "Storage Delete",
		Description: "Delete a file from matrixclaw local storage. Ask for approval before deleting user documents.",
		Risk:        tools.RiskApproval,
		Namespace:   "module.storage",
		Category:    tools.CategoryStorage,
		Profiles:    []tools.Profile{tools.ProfileStorage},
		OutputKind:  tools.OutputStorageEntry,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"}
  },
  "required": ["path"],
  "additionalProperties": false
}`),
	}
}

func (t *deleteTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.store == nil {
		return tools.Result{Content: "Local storage is not configured.", IsError: true}, nil
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid storage_delete arguments.", IsError: true}, nil
	}
	path := strings.TrimSpace(input.Path)
	if !call.Approved {
		return tools.Result{
			Content: "Approval required",
			Approval: &tools.ApprovalRequest{
				ToolID:      "storage_delete",
				Action:      "delete_storage_file",
				Path:        path,
				Description: "Delete storage file " + path,
				Params:      input,
			},
		}, nil
	}
	entry, err := t.store.Delete(path)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Storage delete failed: %s", err), IsError: true}, nil
	}
	return tools.Result{
		Content:  fmt.Sprintf("Deleted from storage: %s", entry.Path),
		Metadata: entry,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func (t *saveTemporaryTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          "storage_save_temp",
		Name:        "Storage Save Temporary File",
		Description: "Copy a temporary upload or attachment into permanent matrixclaw local storage. Use when the user asks to keep an uploaded image or file.",
		Risk:        tools.RiskSafe,
		Namespace:   "module.storage",
		Category:    tools.CategoryStorage,
		Profiles:    []tools.Profile{tools.ProfileStorage},
		OutputKind:  tools.OutputStorageEntry,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "temp_path": {"type": "string", "description": "Temporary storage path shown in the message attachment metadata"},
    "dest_path": {"type": "string", "description": "Permanent relative storage path, for example documents/photo.jpg"}
  },
  "required": ["temp_path", "dest_path"],
  "additionalProperties": false
}`),
	}
}

func (t *saveTemporaryTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.store == nil {
		return tools.Result{Content: "Local storage is not configured.", IsError: true}, nil
	}
	var input struct {
		TempPath string `json:"temp_path"`
		DestPath string `json:"dest_path"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid storage_save_temp arguments.", IsError: true}, nil
	}
	entry, err := t.store.CopyTemporary(input.TempPath, input.DestPath)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Storage temporary save failed: %s", err), IsError: true}, nil
	}
	return tools.Result{
		Content:  fmt.Sprintf("Saved temporary file to storage: %s", entry.Path),
		Metadata: entry,
		MIMEType: entry.MIMEType,
		Status:   tools.ResultStatusSuccess,
	}, nil
}
