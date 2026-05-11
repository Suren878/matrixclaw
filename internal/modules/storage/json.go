package storage

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

func saveJSON(path string, value any) error {
	if err := ensureJSONDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := writeJSONFile(tmp, data); err != nil {
		return err
	}
	return replaceJSONFile(tmp, path)
}

func loadJSON[T any](path string, target *T) error {
	if err := ensureJSONPath(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, target)
}

func ensureJSONDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return os.MkdirAll(dir, 0o755)
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	if !info.IsDir() {
		return fs.ErrInvalid
	}
	return nil
}

func ensureJSONPath(path string) error {
	if err := ensureJSONDir(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	if info.IsDir() {
		return fs.ErrInvalid
	}
	return nil
}

func writeJSONFile(path string, data []byte) error {
	if err := ensureFinalWriteTarget(path, path, true); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func replaceJSONFile(tmp string, path string) error {
	if err := ensureFinalWriteTarget(tmp, tmp, false); err != nil {
		return err
	}
	if err := ensureFinalWriteTarget(path, path, true); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadJSONMap[T any](path string) (map[string]T, error) {
	var m map[string]T
	if err := loadJSON(path, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]T{}
	}
	return m, nil
}
