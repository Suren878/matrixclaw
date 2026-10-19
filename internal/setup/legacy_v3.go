package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/Suren878/matrixclaw/internal/externalagents/codexapp"
	"github.com/Suren878/matrixclaw/internal/providers"
)

// convertV3 converts a version 3 setup.json, which v0.2.0 read through
// aliases, to version 4 in memory; any other file is returned as it is. A
// result that would not load is an error.
func convertV3(data []byte) ([]byte, error) {
	if !isVersion3(data) {
		return data, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	rewriteV3(doc)
	doc["version"] = CurrentVersion
	converted, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if _, err := decodeConfig(converted); err != nil {
		return nil, fmt.Errorf("convert version 3: %w", err)
	}
	return converted, nil
}

func isVersion3(data []byte) bool {
	var probe struct {
		Version int `json:"version"`
	}
	return json.Unmarshal(data, &probe) == nil && probe.Version == 3
}

// Upgrade saves a version 3 file as version 4, keeping the original as
// <path>.pre-v4 (an existing copy is kept). Only the daemon calls it: other
// readers convert in memory, so a new client leaves an older running daemon's
// file alone.
func (s *FileStore) Upgrade() error {
	data, err := os.ReadFile(s.path)
	if err != nil || !isVersion3(data) {
		return nil
	}
	cfg, err := s.Load()
	if err != nil {
		return err
	}
	return s.Save(cfg)
}

func writeBackupOnce(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("back up setup config: %w", err)
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("back up setup config: %w", err)
	}
	return nil
}

// rewriteV3 drops the keys v0.2.0 still wrote and no version reads, and
// replaces the values v0.2.0 accepted as aliases with their canonical form.
func rewriteV3(doc map[string]any) {
	delete(doc, "completed_at")
	delete(object(doc, "clients"), "terminal")
	for _, item := range array(doc, "providers") {
		provider, _ := item.(map[string]any)
		delete(provider, "catalog_id")
		rewrite(provider, "type", providers.NormalizeOptionalProviderType)
		rewrite(provider, "reasoning_effort", providers.NormalizeReasoningEffort)
		rewrite(provider, "tool_use_mode", func(v string) string {
			return string(providers.NormalizeOptionalToolUseMode(providers.ToolUseMode(v)))
		})
	}
	modules := object(doc, "modules")
	delete(object(modules, "skills"), "self_improve")

	search := object(modules, "web_search")
	rewrite(search, "provider", normalizeWebSearchProvider)
	if key, _ := search["api_key"].(string); strings.TrimSpace(key) != "" {
		switch search["provider"] {
		case WebSearchProviderTavily:
			setIfEmpty(search, "tavily_key", key)
		case WebSearchProviderSerper:
			setIfEmpty(search, "serper_key", key)
		}
	}
	delete(search, "api_key")

	browser := object(modules, "browser")
	rewrite(browser, "provider_id", func(string) string { return BrowserProviderPlaywright })
	rewrite(object(browser, "provider_config"), "runtime_mode", runtimeModeV3)

	for _, moduleID := range []string{VoiceModuleTTS, VoiceModuleSTT, "realtime_voice"} {
		module := object(modules, moduleID)
		rewrite(module, "provider_id", normalizeVoiceProviderID)
		if voices := object(module, "providers"); voices != nil {
			canonical := map[string]any{}
			for id, value := range voices {
				id = normalizeVoiceProviderID(id)
				if moduleID != "realtime_voice" && !voiceProviderExists(moduleID, id) {
					continue
				}
				voice, _ := value.(map[string]any)
				rewrite(voice, "runtime_mode", runtimeModeV3)
				switch id {
				case "piper":
					rewrite(voice, "language", piperLanguageV3)
				case "supertonic":
					rewrite(voice, "language", NormalizeSupertonicLanguage)
				}
				canonical[id] = voice
			}
			module["providers"] = canonical
		}
		if id, _ := module["provider_id"].(string); moduleID != "realtime_voice" && id != "" && !voiceProviderExists(moduleID, id) {
			delete(module, "provider_id")
		}
	}

	if agents := object(modules, "external_agents"); agents != nil {
		canonical := map[string]any{}
		for id, value := range agents {
			id = normalizeExternalAgentID(id)
			if id == "codex" {
				if _, exists := agents[codexapp.AgentID]; exists {
					continue
				}
				id = codexapp.AgentID
			}
			canonical[id] = value
		}
		modules["external_agents"] = canonical
	}

	for _, item := range array(object(modules, "mcp"), "servers") {
		server, _ := item.(map[string]any)
		rewrite(server, "transport", func(v string) string {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "http", "streamable_http", "streamable-http":
				return "http"
			default:
				return "stdio"
			}
		})
	}
}

func runtimeModeV3(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "always", "always_running", "persistent", "server":
		return "always_running"
	default:
		return "per_task"
	}
}

func piperLanguageV3(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "english":
		return "en_US"
	case "russian":
		return "ru_RU"
	}
	return NormalizeVoiceLanguage(value)
}

// object is the object under key, nil when there is none; reads and deletes
// on a nil map are no-ops.
func object(parent map[string]any, key string) map[string]any {
	child, _ := parent[key].(map[string]any)
	return child
}

func array(parent map[string]any, key string) []any {
	items, _ := parent[key].([]any)
	return items
}

// rewrite replaces a non-empty string under key with normalize's result.
func rewrite(parent map[string]any, key string, normalize func(string) string) {
	if value, ok := parent[key].(string); ok && value != "" {
		parent[key] = normalize(value)
	}
}

func setIfEmpty(parent map[string]any, key, value string) {
	if current, _ := parent[key].(string); strings.TrimSpace(current) == "" {
		parent[key] = value
	}
}
