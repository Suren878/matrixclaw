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

	data, err = s.upgradeV3(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", s.path, err)
	}
	cfg, err := decodeConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", s.path, err)
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
