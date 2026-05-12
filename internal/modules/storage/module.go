package storage

import (
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const moduleID = "storage.local"

type Config struct {
	Root     string
	MaxBytes int64
}

type Module struct {
	store *LocalStore
}

func New(cfg Config) (*Module, error) {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 25 * 1024 * 1024
	}
	store, err := NewLocalStore(filepath.Clean(cfg.Root), cfg.MaxBytes)
	if err != nil {
		return nil, err
	}
	return &Module{store: store}, nil
}

func (m *Module) ID() string {
	return moduleID
}

func (m *Module) Name() string {
	return "Local Storage"
}

func (m *Module) Store() *LocalStore {
	if m == nil {
		return nil
	}
	return m.store
}

func (m *Module) RegisterTools(registry *tools.Registry) error {
	if m == nil || m.store == nil || registry == nil {
		return nil
	}
	return registry.Register(
		NewSaveTool(m.store),
		NewReadTool(m.store),
		NewListTool(m.store),
		NewUpdateMetadataTool(m.store),
		NewDeleteTool(m.store),
		NewSaveTemporaryTool(m.store),
	)
}

func (m *Module) Context() string {
	if m == nil || m.store == nil {
		return ""
	}
	return strings.TrimSpace(`Local storage is available. Use storage_save to save user documents or generated text into matrixclaw storage, storage_list to find saved files by path/title/tags/query, and storage_read to retrieve saved text. Uploaded files may appear in messages as temp_path values; use storage_save_temp with temp_path and a permanent dest_path when the user asks to keep an uploaded image or file.`)
}
