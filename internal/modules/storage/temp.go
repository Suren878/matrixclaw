package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"
)

type TempEntry struct {
	Path      string    `json:"path"`
	Title     string    `json:"title,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	MIMEType  string    `json:"mime_type,omitempty"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type TempSettings struct {
	AutoCleanup bool  `json:"auto_cleanup"`
	TTLSeconds  int64 `json:"ttl_seconds"`
	MaxBytes    int64 `json:"max_bytes"`
	TotalBytes  int64 `json:"total_bytes"`
	TotalFiles  int   `json:"total_files"`
}

type tempSettingsFile struct {
	AutoCleanup *bool `json:"auto_cleanup,omitempty"`
	TTLSeconds  int64 `json:"ttl_seconds"`
	MaxBytes    int64 `json:"max_bytes"`
}

type TempListResult struct {
	Root     string       `json:"root"`
	Files    []TempEntry  `json:"files"`
	Settings TempSettings `json:"settings"`
}

type CleanupResult struct {
	DeletedFiles int   `json:"deleted_files"`
	FreedBytes   int64 `json:"freed_bytes"`
}

func (s *LocalStore) SaveTemporary(rawPath string, content []byte, title string, tags []string, mimeType string) (TempEntry, error) {
	if int64(len(content)) > s.maxBytes {
		return TempEntry{}, fmt.Errorf("content is too large: %d bytes exceeds %d", len(content), s.maxBytes)
	}
	cleanPath, err := cleanStoragePath(rawPath)
	if err != nil {
		return TempEntry{}, err
	}
	now := time.Now().UTC()
	entry := TempEntry{
		Path:      cleanPath,
		Title:     strings.TrimSpace(title),
		Tags:      normalizeTags(tags),
		MIMEType:  strings.TrimSpace(mimeType),
		Size:      int64(len(content)),
		CreatedAt: now,
		ExpiresAt: now.Add(s.tempTTL),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadTemporarySettingsLocked(); err != nil {
		return TempEntry{}, err
	}
	if s.tempMaxBytes > 0 && entry.Size > s.tempMaxBytes {
		return TempEntry{}, fmt.Errorf("temporary content is too large: %d bytes exceeds %d", entry.Size, s.tempMaxBytes)
	}

	index, err := s.loadTempIndexLocked()
	if err != nil {
		return TempEntry{}, err
	}
	if s.tempAuto {
		if _, err := s.cleanupExpiredTemporaryLocked(index, now); err != nil {
			return TempEntry{}, err
		}
	}

	if previous := index[cleanPath]; previous.Path != "" {
		entry.CreatedAt = previous.CreatedAt
	}
	absPath, err := s.tempFileForWrite(cleanPath)
	if err != nil {
		return TempEntry{}, err
	}
	if err := writeCheckedFile(absPath, content, 0o600, cleanPath); err != nil {
		return TempEntry{}, err
	}
	index[cleanPath] = entry
	if s.tempAuto && s.tempMaxBytes > 0 {
		if err := s.enforceTemporaryLimitLocked(index); err != nil {
			return TempEntry{}, err
		}
	}
	if err := s.saveTempIndexLocked(index); err != nil {
		return TempEntry{}, err
	}
	return entry, nil
}

func (s *LocalStore) ListTemporary(limit int) (TempListResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadTemporarySettingsLocked(); err != nil {
		return TempListResult{}, err
	}

	index, err := s.loadTempIndexLocked()
	if err != nil {
		return TempListResult{}, err
	}
	if s.tempAuto {
		if _, err := s.cleanupExpiredTemporaryLocked(index, time.Now().UTC()); err != nil {
			return TempListResult{}, err
		}
	}
	entries := tempEntries(index)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	if err := s.saveTempIndexLocked(index); err != nil {
		return TempListResult{}, err
	}
	return TempListResult{
		Root:     s.tempRoot,
		Files:    entries,
		Settings: s.tempSettingsLocked(index),
	}, nil
}

func (s *LocalStore) DeleteTemporary(rawPath string) (TempEntry, error) {
	cleanPath, err := cleanStoragePath(rawPath)
	if err != nil {
		return TempEntry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadTempIndexLocked()
	if err != nil {
		return TempEntry{}, err
	}
	entry, ok := index[cleanPath]
	if !ok {
		return TempEntry{}, fmt.Errorf("temporary file not found: %s", cleanPath)
	}
	if err := s.deleteTemporaryLocked(index, cleanPath); err != nil {
		return TempEntry{}, err
	}
	if err := s.saveTempIndexLocked(index); err != nil {
		return TempEntry{}, err
	}
	return entry, nil
}

func (s *LocalStore) CleanupTemporary() (CleanupResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadTemporarySettingsLocked(); err != nil {
		return CleanupResult{}, err
	}

	index, err := s.loadTempIndexLocked()
	if err != nil {
		return CleanupResult{}, err
	}
	result := s.cleanupAllTemporaryLocked(index)
	if err := s.saveTempIndexLocked(index); err != nil {
		return CleanupResult{}, err
	}
	return result, nil
}

func (s *LocalStore) UpdateTemporarySettings(autoCleanup *bool, ttlDays int64, maxGB float64) (TempSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadTemporarySettingsLocked(); err != nil {
		return TempSettings{}, err
	}
	if autoCleanup != nil {
		s.tempAuto = *autoCleanup
	}
	if ttlDays > 0 {
		s.tempTTL = time.Duration(ttlDays) * 24 * time.Hour
	}
	if maxGB > 0 {
		s.tempMaxBytes = int64(maxGB * 1024 * 1024 * 1024)
	}
	if err := s.saveTemporarySettingsLocked(); err != nil {
		return TempSettings{}, err
	}
	index, err := s.loadTempIndexLocked()
	if err != nil {
		return TempSettings{}, err
	}
	if s.tempAuto && s.tempMaxBytes > 0 {
		if _, err := s.cleanupExpiredTemporaryLocked(index, time.Now().UTC()); err != nil {
			return TempSettings{}, err
		}
		if err := s.enforceTemporaryLimitLocked(index); err != nil {
			return TempSettings{}, err
		}
	}
	if err := s.saveTempIndexLocked(index); err != nil {
		return TempSettings{}, err
	}
	return s.tempSettingsLocked(index), nil
}

func (s *LocalStore) PromoteTemporary(rawPath string, destPath string) (Entry, error) {
	entry, content, err := s.readTemporaryBytes(rawPath)
	if err != nil {
		return Entry{}, err
	}
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		destPath = entry.Path
	}
	permanent, err := s.SaveBytes(destPath, content, entry.Title, entry.Tags, entry.MIMEType)
	if err != nil {
		return Entry{}, err
	}
	if _, err := s.DeleteTemporary(entry.Path); err != nil {
		return Entry{}, err
	}
	return permanent, nil
}

func (s *LocalStore) CopyTemporary(rawPath string, destPath string) (Entry, error) {
	entry, content, err := s.readTemporaryBytes(rawPath)
	if err != nil {
		return Entry{}, err
	}
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		destPath = entry.Path
	}
	return s.SaveBytes(destPath, content, entry.Title, entry.Tags, entry.MIMEType)
}

func (s *LocalStore) ReadTemporaryBytes(rawPath string) (TempEntry, []byte, error) {
	return s.readTemporaryBytes(rawPath)
}

func (s *LocalStore) readTemporaryBytes(rawPath string) (TempEntry, []byte, error) {
	cleanPath, err := cleanStoragePath(rawPath)
	if err != nil {
		return TempEntry{}, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadTemporarySettingsLocked(); err != nil {
		return TempEntry{}, nil, err
	}

	index, err := s.loadTempIndexLocked()
	if err != nil {
		return TempEntry{}, nil, err
	}
	entry, ok := index[cleanPath]
	if !ok {
		return TempEntry{}, nil, fmt.Errorf("temporary file not found: %s", cleanPath)
	}
	if s.tempAuto && time.Now().UTC().After(entry.ExpiresAt) {
		_ = s.deleteTemporaryLocked(index, cleanPath)
		_ = s.saveTempIndexLocked(index)
		return TempEntry{}, nil, fmt.Errorf("temporary file expired: %s", cleanPath)
	}
	file, err := s.tempFileForRead(cleanPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			delete(index, cleanPath)
			_ = s.saveTempIndexLocked(index)
			return TempEntry{}, nil, fmt.Errorf("temporary file not found: %s", cleanPath)
		}
		return TempEntry{}, nil, err
	}
	if file.info.IsDir() {
		return TempEntry{}, nil, fmt.Errorf("temporary path is a directory: %s", cleanPath)
	}
	data, err := os.ReadFile(file.abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			delete(index, cleanPath)
			_ = s.saveTempIndexLocked(index)
			return TempEntry{}, nil, fmt.Errorf("temporary file not found: %s", cleanPath)
		}
		return TempEntry{}, nil, err
	}
	return entry, data, nil
}

func (s *LocalStore) loadTempIndexLocked() (map[string]TempEntry, error) {
	return loadJSONMap[TempEntry](s.tempIndex)
}

func (s *LocalStore) loadTemporarySettingsLocked() error {
	var cfg tempSettingsFile
	if err := loadJSON(s.tempSettings, &cfg); err != nil {
		return err
	}
	if cfg.TTLSeconds > 0 {
		s.tempTTL = time.Duration(cfg.TTLSeconds) * time.Second
	}
	if cfg.MaxBytes > 0 {
		s.tempMaxBytes = cfg.MaxBytes
	}
	if cfg.AutoCleanup != nil {
		s.tempAuto = *cfg.AutoCleanup
	}
	return nil
}

func (s *LocalStore) saveTemporarySettingsLocked() error {
	return saveJSON(s.tempSettings, tempSettingsFile{
		AutoCleanup: &s.tempAuto,
		TTLSeconds:  int64(s.tempTTL.Seconds()),
		MaxBytes:    s.tempMaxBytes,
	})
}

func (s *LocalStore) saveTempIndexLocked(index map[string]TempEntry) error {
	if err := s.ensureTempRoot(); err != nil {
		return err
	}
	return saveJSON(s.tempIndex, index)
}

func (s *LocalStore) cleanupExpiredTemporaryLocked(index map[string]TempEntry, now time.Time) (CleanupResult, error) {
	var result CleanupResult
	for path, entry := range index {
		if now.Before(entry.ExpiresAt) {
			continue
		}
		result.DeletedFiles++
		result.FreedBytes += entry.Size
		if err := s.deleteTemporaryLocked(index, path); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *LocalStore) cleanupAllTemporaryLocked(index map[string]TempEntry) CleanupResult {
	var result CleanupResult
	for path, entry := range index {
		result.DeletedFiles++
		result.FreedBytes += entry.Size
		_ = s.deleteTemporaryLocked(index, path)
	}
	return result
}

func (s *LocalStore) enforceTemporaryLimitLocked(index map[string]TempEntry) error {
	entries := tempEntries(index)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})
	total := int64(0)
	for _, entry := range entries {
		total += entry.Size
	}
	for _, entry := range entries {
		if total <= s.tempMaxBytes {
			break
		}
		total -= entry.Size
		if err := s.deleteTemporaryLocked(index, entry.Path); err != nil {
			return err
		}
	}
	return nil
}

func (s *LocalStore) deleteTemporaryLocked(index map[string]TempEntry, cleanPath string) error {
	file, err := s.tempFileForDelete(cleanPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			delete(index, cleanPath)
			return nil
		}
		return err
	}
	if err := removeCheckedFile(file, cleanPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	delete(index, cleanPath)
	return nil
}

func (s *LocalStore) tempSettingsLocked(index map[string]TempEntry) TempSettings {
	total := int64(0)
	for _, entry := range index {
		total += entry.Size
	}
	return TempSettings{
		AutoCleanup: s.tempAuto,
		TTLSeconds:  int64(s.tempTTL.Seconds()),
		MaxBytes:    s.tempMaxBytes,
		TotalBytes:  total,
		TotalFiles:  len(index),
	}
}

func tempEntries(index map[string]TempEntry) []TempEntry {
	entries := make([]TempEntry, 0, len(index))
	for _, entry := range index {
		entries = append(entries, entry)
	}
	return entries
}
