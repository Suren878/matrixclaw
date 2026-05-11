package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrConfigNotFound = errors.New("setup config not found")
var ErrUnsupportedConfigVersion = errors.New("unsupported setup config version")
var ErrDraftNotFound = errors.New("setup draft not found")

type Store interface {
	Load() (Config, error)
	Save(cfg Config) error
	LoadDraft() (Draft, error)
	SaveDraft(draft Draft) error
	ClearDraft() error
	Path() string
}

type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Path() string {
	return s.path
}

func (s *FileStore) Load() (Config, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, ErrConfigNotFound
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Version != CurrentVersion {
		return Config{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedConfigVersion, cfg.Version, CurrentVersion)
	}
	return normalizeConfig(cfg), nil
}

func (s *FileStore) Save(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	cfg = normalizeConfig(cfg)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

type persistedDraft struct {
	Version int   `json:"version"`
	Draft   Draft `json:"draft"`
}

func (s *FileStore) draftPath() string {
	return s.path + ".draft"
}

func (s *FileStore) LoadDraft() (Draft, error) {
	data, err := os.ReadFile(s.draftPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Draft{}, ErrDraftNotFound
		}
		return Draft{}, err
	}

	var payload persistedDraft
	if err := json.Unmarshal(data, &payload); err != nil {
		return Draft{}, err
	}
	if payload.Version != CurrentVersion {
		return Draft{}, ErrDraftNotFound
	}
	return payload.Draft, nil
}

func (s *FileStore) SaveDraft(draft Draft) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(persistedDraft{
		Version: CurrentVersion,
		Draft:   draft,
	}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := s.draftPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.draftPath())
}

func (s *FileStore) ClearDraft() error {
	err := os.Remove(s.draftPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
