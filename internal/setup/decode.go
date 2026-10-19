package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/externalagents/claudecode"
	"github.com/Suren878/matrixclaw/internal/externalagents/codexapp"
	"github.com/Suren878/matrixclaw/internal/providers"
)

// decodeConfig reads setup.json strictly: a key this version does not know
// or a value outside a closed list is an error naming the key, so a file an
// older version wrote fails loudly instead of being reinterpreted.
func decodeConfig(data []byte) (Config, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, err
	}
	if unknown := unknownKeys(raw, reflect.TypeFor[Config](), ""); len(unknown) > 0 {
		return Config{}, fmt.Errorf("unknown keys %s: this version of matrixclaw does not read them; remove them", strings.Join(unknown, ", "))
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Version != CurrentVersion {
		return Config{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedConfigVersion, cfg.Version, CurrentVersion)
	}
	if err := checkStoredValues(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// unknownKeys lists the keys of raw, by path, that no field of t declares.
func unknownKeys(raw any, t reflect.Type, path string) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var unknown []string
	switch t.Kind() {
	case reflect.Struct:
		object, ok := raw.(map[string]any)
		if !ok {
			return nil
		}
		fields := map[string]reflect.Type{}
		for i := range t.NumField() {
			field := t.Field(i)
			if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); name != "" && name != "-" {
				fields[name] = field.Type
			}
		}
		for _, key := range slices.Sorted(maps.Keys(object)) {
			child := key
			if path != "" {
				child = path + "." + key
			}
			fieldType, ok := fields[key]
			if !ok {
				unknown = append(unknown, child)
				continue
			}
			unknown = append(unknown, unknownKeys(object[key], fieldType, child)...)
		}
	case reflect.Map:
		object, _ := raw.(map[string]any)
		for _, key := range slices.Sorted(maps.Keys(object)) {
			unknown = append(unknown, unknownKeys(object[key], t.Elem(), path+"."+key)...)
		}
	case reflect.Slice:
		items, _ := raw.([]any)
		for i, item := range items {
			unknown = append(unknown, unknownKeys(item, t.Elem(), path+"["+strconv.Itoa(i)+"]")...)
		}
	}
	return unknown
}

// runtimeModes are the stored runtime modes of local browser and voice runtimes.
var runtimeModes = []string{"per_task", "always_running"}

// checkStoredValues rejects stored values outside their closed lists, hooks
// that cannot run, diagnostics keys that are not file extensions and sandbox
// paths that are not absolute.
func checkStoredValues(cfg Config) error {
	var problems []error
	check := func(key, value string, allowed ...string) {
		if value != "" && !slices.Contains(allowed, value) {
			problems = append(problems, fmt.Errorf("%s is %q, want one of %q", key, value, allowed))
		}
	}
	canonical := func(key, value, want string) {
		if value != want {
			problems = append(problems, fmt.Errorf("%s is %q, want %q", key, value, want))
		}
	}
	for i, provider := range cfg.Providers {
		key := "providers[" + strconv.Itoa(i) + "]."
		canonical(key+"type", provider.Type, providers.NormalizeOptionalProviderType(provider.Type))
		canonical(key+"reasoning_effort", provider.ReasoningEffort, providers.NormalizeReasoningEffort(provider.ReasoningEffort))
		check(key+"tool_use_mode", string(provider.ToolUseMode), string(providers.ToolUseNative), string(providers.ToolUseDisabled))
	}
	modules := cfg.Modules
	for _, id := range slices.Sorted(maps.Keys(modules.ExternalAgents)) {
		check("modules.external_agents key", id, claudecode.AgentID, codexapp.AgentID)
	}
	for _, voice := range []struct {
		id     string
		module VoiceModuleConfig
	}{{VoiceModuleTTS, modules.TextToSpeech}, {VoiceModuleSTT, modules.SpeechToText}} {
		moduleID, module := voice.id, voice.module
		key := "modules." + moduleID
		check(key+".provider_id", module.ProviderID, VoiceProviderIDs(moduleID)...)
		for _, id := range slices.Sorted(maps.Keys(module.Providers)) {
			provider := module.Providers[id]
			check(key+".providers key", id, VoiceProviderIDs(moduleID)...)
			check(key+".providers."+id+".runtime_mode", provider.RuntimeMode, runtimeModes...)
			switch {
			case id == "piper" && provider.Language != "":
				if !strings.Contains(provider.Language, "_") || NormalizeVoiceLanguage(provider.Language) != provider.Language {
					problems = append(problems, fmt.Errorf("%s.providers.piper.language is %q, want a code such as \"en_US\"", key, provider.Language))
				}
			case id == "supertonic" && provider.Language != "":
				canonical(key+".providers.supertonic.language", provider.Language, NormalizeSupertonicLanguage(provider.Language))
			}
		}
	}
	check("modules.browser.provider_id", modules.Browser.ProviderID, BrowserProviderPlaywright)
	check("modules.browser.provider_config.runtime_mode", modules.Browser.ProviderConfig.RuntimeMode, runtimeModes...)
	check("modules.web_search.provider", modules.WebSearch.Provider, WebSearchProviderDDG, WebSearchProviderTavily, WebSearchProviderSerper, WebSearchProviderSearXNG)
	for i, server := range modules.MCP.Servers {
		check("modules.mcp.servers["+strconv.Itoa(i)+"].transport", server.Transport, "stdio", "http")
	}
	for i, hook := range cfg.Hooks {
		if err := hook.Validate(); err != nil {
			problems = append(problems, fmt.Errorf("hooks[%d]: %w", i, err))
		}
	}
	if err := cfg.Diagnostics.Validate(); err != nil {
		problems = append(problems, fmt.Errorf("diagnostics: %w", err))
	}
	if err := cfg.Daemon.Sandbox.Validate(); err != nil {
		problems = append(problems, fmt.Errorf("daemon.sandbox: %w", err))
	}
	return errors.Join(problems...)
}
