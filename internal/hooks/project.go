package hooks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ProjectFile is where a project keeps its hooks, relative to its directory.
const ProjectFile = ".matrixclaw/hooks.json"

// ProjectHooks is a project's hooks file as the owner sees it: hooks run only
// while Trusted, which holds while the file keeps the SHA256 the owner trusted.
type ProjectHooks struct {
	Path    string `json:"path"`
	Dir     string `json:"dir"`
	SHA256  string `json:"sha256"`
	Trusted bool   `json:"trusted"`
	Hooks   []Hook `json:"hooks,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Listing is every hook a session meets: setup.json's and its project's.
type Listing struct {
	Hooks   []Hook        `json:"hooks"`
	Project *ProjectHooks `json:"project,omitempty"`
}

// findProjectFile is the nearest ProjectFile from dir up to the root of the
// git repository dir is in; outside a repository only dir is looked at.
func findProjectFile(dir string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	top := dir
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			top = d
			break
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	for d := dir; ; d = filepath.Dir(d) {
		path := filepath.Join(d, ProjectFile)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
		if d == top || filepath.Dir(d) == d {
			return ""
		}
	}
}

// readProject reads the project hooks file found from dir; nil when there is none.
func readProject(dir string, trust *trustStore) *ProjectHooks {
	path := findProjectFile(dir)
	if path == "" {
		return nil
	}
	project := &ProjectHooks{Path: path, Dir: filepath.Dir(filepath.Dir(path))}
	data, err := os.ReadFile(path)
	if err != nil {
		project.Error = err.Error()
		return project
	}
	sum := sha256.Sum256(data)
	project.SHA256 = hex.EncodeToString(sum[:])
	project.Hooks, err = decodeProject(data)
	if err != nil {
		project.Error = err.Error()
		return project
	}
	project.Trusted = trust.trusted(path, project.SHA256)
	return project
}

// decodeProject reads a hooks file strictly: {"hooks": [...]}, every hook valid.
func decodeProject(data []byte) ([]Hook, error) {
	var file struct {
		Hooks []Hook `json:"hooks"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}
	var problems []error
	for i, hook := range file.Hooks {
		if err := hook.Validate(); err != nil {
			problems = append(problems, fmt.Errorf("hooks[%d]: %w", i, err))
		}
	}
	return file.Hooks, errors.Join(problems...)
}

// trustStore keeps, per project hooks file, the SHA256 its owner trusted.
type trustStore struct {
	path string
	mu   sync.Mutex
}

func (s *trustStore) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	trusted := map[string]string{}
	if err := json.Unmarshal(data, &trusted); err != nil {
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}
	return trusted, nil
}

func (s *trustStore) trusted(path, sum string) bool {
	if s == nil || s.path == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	trusted, err := s.load()
	return err == nil && trusted[path] == sum
}

func (s *trustStore) trust(path, sum string) error {
	if s == nil || s.path == "" {
		return errors.New("hooks: no trust store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	trusted, err := s.load()
	if err != nil {
		return err
	}
	trusted[path] = sum
	data, err := json.MarshalIndent(trusted, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
