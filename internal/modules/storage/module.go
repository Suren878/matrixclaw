package storage

import (
	"path/filepath"

	"github.com/Suren878/matrixclaw/internal/tools"
)

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

func (m *Module) Store() *LocalStore {
	if m == nil {
		return nil
	}
	return m.store
}

// Tools are the storage tools.
func (m *Module) Tools() []tools.Executor {
	return []tools.Executor{
		NewSaveTool(m.store),
		NewReadTool(m.store),
		NewListTool(m.store),
		NewUpdateMetadataTool(m.store),
		NewDeleteTool(m.store),
		NewSaveTemporaryTool(m.store),
	}
}
