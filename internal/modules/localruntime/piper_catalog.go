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

const piperVoicesCatalogURL = "https://huggingface.co/rhasspy/piper-voices/resolve/main/voices.json"

var piperCatalogCache struct {
	sync.Mutex
	expires   time.Time
	failUntil time.Time
	models    []setup.VoiceModelOption
}

type piperCatalogEntry struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Language struct {
		Code           string `json:"code"`
		NameEnglish    string `json:"name_english"`
		CountryEnglish string `json:"country_english"`
	} `json:"language"`
	Quality string                      `json:"quality"`
	Files   map[string]piperCatalogFile `json:"files"`
}

type piperCatalogFile struct {
	SizeBytes int64 `json:"size_bytes"`
}

func piperCatalogModels() ([]setup.VoiceModelOption, bool) {
	now := time.Now()
	piperCatalogCache.Lock()
	if now.Before(piperCatalogCache.expires) && len(piperCatalogCache.models) > 0 {
		models := append([]setup.VoiceModelOption(nil), piperCatalogCache.models...)
		piperCatalogCache.Unlock()
		return models, true
	}
	if now.Before(piperCatalogCache.failUntil) {
		piperCatalogCache.Unlock()
		return nil, false
	}
	piperCatalogCache.Unlock()

	models, err := fetchPiperCatalogModels()
	if err != nil || len(models) == 0 {
		piperCatalogCache.Lock()
		piperCatalogCache.failUntil = now.Add(10 * time.Minute)
		piperCatalogCache.Unlock()
		return nil, false
	}

	piperCatalogCache.Lock()
	piperCatalogCache.models = append([]setup.VoiceModelOption(nil), models...)
	piperCatalogCache.expires = now.Add(6 * time.Hour)
	piperCatalogCache.Unlock()
	return models, true
}

func fetchPiperCatalogModels() ([]setup.VoiceModelOption, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(piperVoicesCatalogURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("piper voices catalog: status %s", response.Status)
	}

	var catalog map[string]piperCatalogEntry
	if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
		return nil, err
	}
	models := make([]setup.VoiceModelOption, 0, len(catalog))
	for key, entry := range catalog {
		model := piperCatalogModel(key, entry)
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].LanguageName != models[j].LanguageName {
			return models[i].LanguageName < models[j].LanguageName
		}
		if models[i].Name != models[j].Name {
			return models[i].Name < models[j].Name
		}
		return qualityRank(models[i].Quality) < qualityRank(models[j].Quality)
	})
	return models, nil
}

func piperCatalogModel(key string, entry piperCatalogEntry) setup.VoiceModelOption {
	key = strings.TrimSpace(firstNonEmptyLocal(entry.Key, key))
	if key == "" {
		return setup.VoiceModelOption{}
	}
	quality := strings.TrimSpace(entry.Quality)
	languageName := strings.TrimSpace(entry.Language.NameEnglish)
	languageCode := strings.TrimSpace(entry.Language.Code)
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = key
	}
	return setup.VoiceModelOption{
		ID:           key,
		Name:         titleWords(name) + " " + qualityLabel(quality),
		Size:         formatVoiceModelSize(piperCatalogONNXSize(entry.Files)),
		Description:  strings.TrimSpace(strings.Join(nonEmptyLocal(languageName, strings.TrimSpace(entry.Language.CountryEnglish)), " · ")),
		Default:      key == "en_US-lessac-medium",
		LanguageCode: languageCode,
		LanguageName: languageName,
		Quality:      quality,
	}
}

func piperCatalogONNXSize(files map[string]piperCatalogFile) int64 {
	var size int64
	for path, file := range files {
		if strings.HasSuffix(path, ".onnx") && !strings.HasSuffix(path, ".onnx.json") {
			if file.SizeBytes > size {
				size = file.SizeBytes
			}
		}
	}
	return size
}

func formatVoiceModelSize(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	const mb = 1024 * 1024
	value := (bytes + mb/2) / mb
	if value <= 0 {
		value = 1
	}
	return fmt.Sprintf("~%d MB", value)
}

func titleWords(value string) string {
	value = strings.ReplaceAll(value, "_", " ")
	parts := strings.Fields(value)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func qualityLabel(quality string) string {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "x_low":
		return "X-Low"
	case "low":
		return "Low"
	case "medium":
		return "Medium"
	case "high":
		return "High"
	default:
		return strings.TrimSpace(quality)
	}
}

func qualityRank(quality string) int {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "x_low":
		return 0
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 4
	}
}

func firstNonEmptyLocal(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func nonEmptyLocal(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
