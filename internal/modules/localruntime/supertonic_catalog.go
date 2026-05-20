package localruntime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

const supertonicVoiceStylesURL = "https://huggingface.co/api/models/Supertone/supertonic-3/tree/main/voice_styles"

var supertonicCatalogCache struct {
	sync.Mutex
	models    []setup.VoiceModelOption
	expires   time.Time
	failUntil time.Time
}

type supertonicTreeEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

func supertonicCatalogModels() ([]setup.VoiceModelOption, bool) {
	now := time.Now()
	supertonicCatalogCache.Lock()
	if now.Before(supertonicCatalogCache.expires) && len(supertonicCatalogCache.models) > 0 {
		models := append([]setup.VoiceModelOption(nil), supertonicCatalogCache.models...)
		supertonicCatalogCache.Unlock()
		return models, true
	}
	if now.Before(supertonicCatalogCache.failUntil) {
		supertonicCatalogCache.Unlock()
		return supertonicFallbackModels(), false
	}
	supertonicCatalogCache.Unlock()

	models, err := fetchSupertonicCatalogModels()
	if err != nil || len(models) == 0 {
		supertonicCatalogCache.Lock()
		supertonicCatalogCache.failUntil = now.Add(10 * time.Minute)
		supertonicCatalogCache.Unlock()
		return supertonicFallbackModels(), false
	}

	supertonicCatalogCache.Lock()
	supertonicCatalogCache.models = append([]setup.VoiceModelOption(nil), models...)
	supertonicCatalogCache.expires = now.Add(6 * time.Hour)
	supertonicCatalogCache.Unlock()
	return models, true
}

func fetchSupertonicCatalogModels() ([]setup.VoiceModelOption, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Get(supertonicVoiceStylesURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Supertonic voice style catalog: status %s", response.Status)
	}
	var entries []supertonicTreeEntry
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		return nil, err
	}
	models := make([]setup.VoiceModelOption, 0, len(entries))
	for _, entry := range entries {
		if strings.ToLower(strings.TrimSpace(entry.Type)) != "file" || strings.ToLower(filepath.Ext(entry.Path)) != ".json" {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(entry.Path), filepath.Ext(entry.Path))
		if id == "" {
			continue
		}
		models = append(models, supertonicVoiceModel(id, entry.Size))
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})
	for i := range models {
		if models[i].ID == "M1" {
			models[i].Default = true
			break
		}
	}
	return models, nil
}

func supertonicFallbackModels() []setup.VoiceModelOption {
	return []setup.VoiceModelOption{supertonicVoiceModel("M1", 0)}
}

func supertonicVoiceModel(id string, size int64) setup.VoiceModelOption {
	id = strings.ToUpper(strings.TrimSpace(id))
	name := id
	switch {
	case strings.HasPrefix(id, "M"):
		name = "Male " + strings.TrimPrefix(id, "M")
	case strings.HasPrefix(id, "F"):
		name = "Female " + strings.TrimPrefix(id, "F")
	}
	model := setup.VoiceModelOption{
		ID:          id,
		Name:        name,
		Size:        "~400 MB shared model",
		Description: "Supertonic 3 voice style",
	}
	if size > 0 {
		model.Description = "Supertonic 3 voice style · style " + formatVoiceModelSize(size)
	}
	return model
}

func supertonicVoiceStyleURL(voiceID string) (string, error) {
	voiceID = strings.ToUpper(strings.TrimSpace(voiceID))
	if voiceID == "" {
		voiceID = "M1"
	}
	if strings.ContainsAny(voiceID, `/\`) || strings.Contains(voiceID, "..") {
		return "", fmt.Errorf("Supertonic voice %q does not have a bundled style", voiceID)
	}
	return "https://huggingface.co/Supertone/supertonic-3/resolve/main/voice_styles/" + voiceID + ".json", nil
}
