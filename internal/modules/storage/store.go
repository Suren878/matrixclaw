package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	metadataDirName = ".matrixclaw"
	tempDirName     = "temporary"
	defaultTempTTL  = 7 * 24 * time.Hour
	defaultTempMax  = 5 * 1024 * 1024 * 1024
)

var ErrInvalidPath = errors.New("invalid storage path")

type Entry struct {
	Path      string    `json:"path"`
	Title     string    `json:"title,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	MIMEType  string    `json:"mime_type,omitempty"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ListFilter struct {
	Prefix string
	Query  string
	Limit  int
}

type ListResult struct {
	Root  string  `json:"root"`
	Files []Entry `json:"files"`
}

type ReadResult struct {
	File    Entry  `json:"file"`
	Content string `json:"content"`
}

type LocalStore struct {
	root         string
	index        string
	tempRoot     string
	tempIndex    string
	tempSettings string
	tempTTL      time.Duration
	tempMaxBytes int64
	tempAuto     bool
	maxBytes     int64
	mu           sync.Mutex
}

func NewLocalStore(root string, maxBytes int64) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("storage root is required")
	}
	if maxBytes <= 0 {
		maxBytes = 25 * 1024 * 1024
	}
	resolvedRoot, err := resolveStoreRoot(root)
	if err != nil {
		return nil, err
	}
	store := &LocalStore{
		root:         resolvedRoot,
		index:        filepath.Join(resolvedRoot, metadataDirName, "index.json"),
		tempRoot:     filepath.Join(resolvedRoot, tempDirName),
		tempIndex:    filepath.Join(resolvedRoot, metadataDirName, "temporary.json"),
		tempSettings: filepath.Join(resolvedRoot, metadataDirName, "temporary_settings.json"),
		tempTTL:      defaultTempTTL,
		tempMaxBytes: defaultTempMax,
		tempAuto:     true,
		maxBytes:     maxBytes,
	}
	if err := store.ensureMetadataRoot(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *LocalStore) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *LocalStore) Save(rawPath string, content string, title string, tags []string, mimeType string) (Entry, error) {
	return s.SaveBytes(rawPath, []byte(content), title, tags, mimeType)
}

func (s *LocalStore) SaveBytes(rawPath string, content []byte, title string, tags []string, mimeType string) (Entry, error) {
	if int64(len(content)) > s.maxBytes {
		return Entry{}, fmt.Errorf("content is too large: %d bytes exceeds %d", len(content), s.maxBytes)
	}
	cleanPath, err := cleanStoragePath(rawPath)
	if err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadIndexLocked()
	if err != nil {
		return Entry{}, err
	}
	entry := index[cleanPath]
	if entry.Path == "" {
		entry.Path = cleanPath
		entry.CreatedAt = now
	}
	entry.Title = strings.TrimSpace(title)
	entry.Tags = normalizeTags(tags)
	entry.MIMEType = strings.TrimSpace(mimeType)
	entry.Size = int64(len(content))
	entry.UpdatedAt = now

	absPath, err := s.fileForWrite(cleanPath)
	if err != nil {
		return Entry{}, err
	}
	if err := writeCheckedFile(absPath, content, 0o600, cleanPath); err != nil {
		return Entry{}, err
	}
	index[cleanPath] = entry
	if err := s.saveIndexLocked(index); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *LocalStore) UpdateMetadata(rawPath string, title string, tags []string, mimeType string) (Entry, error) {
	cleanPath, err := cleanStoragePath(rawPath)
	if err != nil {
		return Entry{}, err
	}
	file, err := s.fileForRead(cleanPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Entry{}, fmt.Errorf("storage file not found: %s", cleanPath)
		}
		return Entry{}, err
	}
	info := file.info
	if info.IsDir() {
		return Entry{}, fmt.Errorf("storage path is a directory: %s", cleanPath)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.loadIndexLocked()
	if err != nil {
		return Entry{}, err
	}
	entry := index[cleanPath]
	if entry.Path == "" {
		entry = Entry{
			Path:      cleanPath,
			CreatedAt: info.ModTime().UTC(),
		}
	}
	entry.Title = strings.TrimSpace(title)
	entry.Tags = normalizeTags(tags)
	entry.MIMEType = strings.TrimSpace(mimeType)
	entry.Size = info.Size()
	entry.UpdatedAt = time.Now().UTC()
	index[cleanPath] = entry
	if err := s.saveIndexLocked(index); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *LocalStore) Delete(rawPath string) (Entry, error) {
	cleanPath, err := cleanStoragePath(rawPath)
	if err != nil {
		return Entry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadIndexLocked()
	if err != nil {
		return Entry{}, err
	}
	entry := index[cleanPath]
	if entry.Path == "" {
		file, statErr := s.fileForRead(cleanPath)
		if statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				return Entry{}, fmt.Errorf("storage file not found: %s", cleanPath)
			}
			return Entry{}, statErr
		}
		info := file.info
		if info.IsDir() {
			return Entry{}, fmt.Errorf("storage path is a directory: %s", cleanPath)
		}
		entry = Entry{
			Path:      cleanPath,
			Size:      info.Size(),
			CreatedAt: info.ModTime().UTC(),
			UpdatedAt: info.ModTime().UTC(),
		}
	}
	file, err := s.fileForDelete(cleanPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Entry{}, fmt.Errorf("storage file not found: %s", cleanPath)
		}
		return Entry{}, err
	}
	if err := removeCheckedFile(file, cleanPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Entry{}, fmt.Errorf("storage file not found: %s", cleanPath)
		}
		return Entry{}, err
	}
	delete(index, cleanPath)
	if err := s.saveIndexLocked(index); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *LocalStore) Read(rawPath string, maxBytes int64) (Entry, string, error) {
	entry, data, err := s.readBytes(rawPath, maxBytes)
	return entry, string(data), err
}

func (s *LocalStore) ReadBytes(rawPath string, maxBytes int64) (Entry, []byte, error) {
	return s.readBytes(rawPath, maxBytes)
}

func (s *LocalStore) readBytes(rawPath string, maxBytes int64) (Entry, []byte, error) {
	cleanPath, err := cleanStoragePath(rawPath)
	if err != nil {
		return Entry{}, nil, err
	}
	if maxBytes <= 0 || maxBytes > s.maxBytes {
		maxBytes = s.maxBytes
	}
	file, err := s.fileForRead(cleanPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Entry{}, nil, fmt.Errorf("storage file not found: %s", cleanPath)
		}
		return Entry{}, nil, err
	}
	info := file.info
	if info.IsDir() {
		return Entry{}, nil, fmt.Errorf("storage path is a directory: %s", cleanPath)
	}
	if info.Size() > maxBytes {
		return Entry{}, nil, fmt.Errorf("storage file is too large: %d bytes exceeds %d", info.Size(), maxBytes)
	}

	data, err := os.ReadFile(file.abs)
	if err != nil {
		return Entry{}, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.loadIndexLocked()
	if err != nil {
		return Entry{}, nil, err
	}
	entry := index[cleanPath]
	if entry.Path == "" {
		entry = Entry{
			Path:      cleanPath,
			Size:      info.Size(),
			CreatedAt: info.ModTime().UTC(),
			UpdatedAt: info.ModTime().UTC(),
		}
	} else {
		entry.Size = info.Size()
	}
	return entry, data, nil
}

func (s *LocalStore) List(prefix string, query string, limit int) ([]Entry, error) {
	prefix = strings.Trim(strings.TrimSpace(strings.ReplaceAll(prefix, "\\", "/")), "/")
	query = strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadIndexLocked()
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(index))
	err = filepath.WalkDir(s.root, func(absPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && d.Name() == metadataDirName {
			return filepath.SkipDir
		}
		if d.IsDir() && d.Name() == tempDirName {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.root, absPath)
		if err != nil {
			return err
		}
		cleanPath := filepath.ToSlash(rel)
		file, err := s.fileForRead(cleanPath)
		if err != nil {
			if errors.Is(err, ErrInvalidPath) || errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if file.info.IsDir() {
			return nil
		}
		entry := index[cleanPath]
		if entry.Path == "" {
			entry = Entry{
				Path:      cleanPath,
				Size:      file.info.Size(),
				CreatedAt: file.info.ModTime().UTC(),
				UpdatedAt: file.info.ModTime().UTC(),
			}
		}
		if prefix != "" && !strings.HasPrefix(entry.Path, prefix) {
			return nil
		}
		if query != "" && !entry.matches(query) && !s.fileContainsQuery(file.abs, query) {
			return nil
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (s *LocalStore) loadIndexLocked() (map[string]Entry, error) {
	return loadJSONMap[Entry](s.index)
}

func (s *LocalStore) saveIndexLocked(index map[string]Entry) error {
	return saveJSON(s.index, index)
}

func cleanStoragePath(raw string) (string, error) {
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	value = strings.TrimPrefix(value, "/")
	clean := path.Clean(value)
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrInvalidPath
	}
	first := clean
	if idx := strings.IndexByte(clean, '/'); idx >= 0 {
		first = clean[:idx]
	}
	if first == metadataDirName || first == tempDirName {
		return "", ErrInvalidPath
	}
	return clean, nil
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func (e Entry) matches(query string) bool {
	if strings.Contains(strings.ToLower(e.Path), query) || strings.Contains(strings.ToLower(e.Title), query) {
		return true
	}
	for _, tag := range e.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

func (s *LocalStore) fileContainsQuery(absPath string, query string) bool {
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() || info.Size() > 256*1024 {
		return false
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), query)
}
