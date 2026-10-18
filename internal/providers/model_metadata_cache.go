package providers

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// modelMetadataCacheFile keeps learned metadata by provider ID, then model ID.
type modelMetadataCacheFile struct {
	Models map[string]map[string]ModelMetadataRegistration `json:"models,omitempty"`
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
	for providerID, models := range payload.Models {
		for modelID, value := range models {
			RegisterModelMetadata(providerID, modelID, value)
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
	payload := modelMetadataCacheFile{Models: map[string]map[string]ModelMetadataRegistration{}}
	learnedMetadata.RLock()
	for providerID, models := range learnedMetadata.values {
		if len(models) > 0 {
			payload.Models[providerID] = maps.Clone(models)
		}
	}
	learnedMetadata.RUnlock()
	if len(payload.Models) == 0 {
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
