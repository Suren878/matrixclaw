package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type modelMetadataCacheFile struct {
	ModelMetadata map[string]ModelMetadataRegistration `json:"model_metadata,omitempty"`
}

var modelMetadataCache struct {
	sync.Mutex
	path string
}

// UseModelMetadataCache loads what earlier listings learned from path and makes
// SaveModelMetadata write there; an empty path keeps the metadata in memory.
func UseModelMetadataCache(path string) {
	modelMetadataCache.Lock()
	modelMetadataCache.path = strings.TrimSpace(path)
	path = modelMetadataCache.path
	modelMetadataCache.Unlock()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var payload modelMetadataCacheFile
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	modelMetadataOverrides.Lock()
	defer modelMetadataOverrides.Unlock()
	for key, value := range payload.ModelMetadata {
		if strings.TrimSpace(key) != "" {
			modelMetadataOverrides.values[key] = mergeModelMetadata(modelMetadataOverrides.values[key], value)
		}
	}
}

// SaveModelMetadata persists the registered metadata once a listing is done.
func SaveModelMetadata() {
	modelMetadataCache.Lock()
	defer modelMetadataCache.Unlock()
	path := modelMetadataCache.path
	if path == "" {
		return
	}
	payload := modelMetadataCacheFile{ModelMetadata: map[string]ModelMetadataRegistration{}}
	modelMetadataOverrides.RLock()
	for key, value := range modelMetadataOverrides.values {
		if strings.TrimSpace(key) != "" && modelMetadataKnown(value) {
			payload.ModelMetadata[key] = value
		}
	}
	modelMetadataOverrides.RUnlock()
	if len(payload.ModelMetadata) == 0 {
		return
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".model-metadata-*.json")
	if err != nil {
		return
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, writeErr := tmp.Write(raw)
	if closeErr := tmp.Close(); writeErr != nil || closeErr != nil {
		return
	}
	_ = os.Rename(tmp.Name(), path)
}
