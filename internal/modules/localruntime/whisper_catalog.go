package localruntime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

const whisperCatalogURL = "https://huggingface.co/api/models/ggerganov/whisper.cpp/tree/main"

var whisperCatalogCache struct {
	sync.Mutex
	expires   time.Time
	failUntil time.Time
	models    []setup.VoiceModelOption
}

type whisperCatalogEntry struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func whisperCatalogModels() ([]setup.VoiceModelOption, bool) {
	now := time.Now()
	whisperCatalogCache.Lock()
	if now.Before(whisperCatalogCache.expires) && len(whisperCatalogCache.models) > 0 {
		models := append([]setup.VoiceModelOption(nil), whisperCatalogCache.models...)
		whisperCatalogCache.Unlock()
		return models, true
	}
	if now.Before(whisperCatalogCache.failUntil) {
		whisperCatalogCache.Unlock()
		return nil, false
	}
	whisperCatalogCache.Unlock()

	models, err := fetchWhisperCatalogModels()
	if err != nil || len(models) == 0 {
		whisperCatalogCache.Lock()
		whisperCatalogCache.failUntil = now.Add(10 * time.Minute)
		whisperCatalogCache.Unlock()
		return nil, false
	}

	whisperCatalogCache.Lock()
	whisperCatalogCache.models = append([]setup.VoiceModelOption(nil), models...)
	whisperCatalogCache.expires = now.Add(6 * time.Hour)
	whisperCatalogCache.Unlock()
	return models, true
}

func fetchWhisperCatalogModels() ([]setup.VoiceModelOption, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(whisperCatalogURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("whisper.cpp catalog: status %s", response.Status)
	}
	var entries []whisperCatalogEntry
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		return nil, err
	}
	models := make([]setup.VoiceModelOption, 0, len(entries))
	for _, entry := range entries {
		model := whisperCatalogModel(entry)
		if strings.TrimSpace(model.ID) != "" {
			models = append(models, model)
		}
	}
	sort.Slice(models, func(i, j int) bool {
		ri, rj := whisperModelRank(models[i].ID), whisperModelRank(models[j].ID)
		if ri != rj {
			return ri < rj
		}
		return models[i].ID < models[j].ID
	})
	return models, nil
}

func whisperCatalogModel(entry whisperCatalogEntry) setup.VoiceModelOption {
	path := strings.TrimSpace(entry.Path)
	if entry.Type != "file" || !strings.HasPrefix(path, "ggml-") || !strings.HasSuffix(path, ".bin") {
		return setup.VoiceModelOption{}
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, "ggml-"), ".bin")
	if id == "" || strings.Contains(id, "/") {
		return setup.VoiceModelOption{}
	}
	return setup.VoiceModelOption{
		ID:          id,
		Name:        whisperModelName(id),
		Size:        formatVoiceModelSize(entry.Size),
		RAM:         whisperModelRAM(id),
		Description: "Whisper.cpp model",
		Default:     id == "base",
	}
}

func whisperModelName(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return titleWords(strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(id))
}

func whisperModelRAM(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.TrimSuffix(id, ".en")
	switch id {
	case "tiny":
		return "~390 MB"
	case "base":
		return "~500 MB"
	case "small":
		return "~1 GB"
	case "medium":
		return "~2.6 GB"
	case "large-v1", "large-v2", "large-v3", "large-v3-turbo":
		return "~4 GB"
	default:
		return ""
	}
}

func whisperModelRank(id string) int {
	id = strings.ToLower(strings.TrimSpace(id))
	base := strings.TrimSuffix(id, ".en")
	rank := 100
	switch base {
	case "tiny":
		rank = 0
	case "base":
		rank = 10
	case "small":
		rank = 20
	case "medium":
		rank = 30
	case "large-v1":
		rank = 40
	case "large-v2":
		rank = 50
	case "large-v3":
		rank = 60
	case "large-v3-turbo":
		rank = 70
	}
	if strings.HasSuffix(id, ".en") {
		rank++
	}
	return rank
}
