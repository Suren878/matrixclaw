package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// oldSetupFile is shaped like a file older versions wrote: catalog defaults
// copied into every provider and voice provider, catalog_id keys, and user
// choices that differ from the defaults.
const oldSetupFile = `{
  "version": 3,
  "active_provider_id": "openai",
  "assistant": {"name": "matrixclaw", "custom_instructions": "Be brief."},
  "providers": [
    {"id": "openai", "catalog_id": "openai", "name": "OpenAI", "type": "openai-compatible", "api_key": "sk-openai-1234", "api_key_env": "OPENAI_API_KEY", "base_url": "https://proxy.example/v1", "model": "gpt-5.4", "reasoning_effort": "high", "tool_use_mode": "native"},
    {"id": "qwen", "catalog_id": "qwen", "name": "Qwen / DashScope", "type": "openai-compatible", "api_key": "sk-qwen-1234", "api_key_env": "DASHSCOPE_API_KEY", "base_url": "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "model": "qwen-plus", "reasoning_effort": "medium"},
    {"id": "anthropic", "catalog_id": "anthropic", "name": "Anthropic", "type": "anthropic-compatible", "api_key": "sk-ant-1234", "api_key_env": "ANTHROPIC_API_KEY", "base_url": "https://api.anthropic.com/v1", "model": "claude-sonnet-4-5", "context_window": 150000, "max_output_tokens": 8000},
    {"id": "local-ai", "catalog_id": "local-ai", "name": "Local AI", "type": "openai-compatible", "api_key": "sk-local-1234", "api_key_env": "OPENAI_COMPAT_API_KEY", "base_url": "http://127.0.0.1:8000/v1", "model": "qwen3", "tool_use_mode": "disabled"}
  ],
  "daemon": {
    "http_addr": "127.0.0.1:7777", "db_path": "/var/lib/matrixclaw.db", "timezone": "Europe/Berlin", "api_token": "token-1234", "autostart_on_boot": true,
    "budgets": {"user": {"steps": 500, "active_time": "6h"}, "automation": {"tokens": 200000}},
    "compact_model": {"provider": "openai", "model": "gpt-5.4-mini"},
    "context_window_cap": 100000, "model_concurrency": 2, "background_tasks": 3, "background_agents": 2
  },
  "clients": {"telegram": {"enabled": true, "bot_token": "123:abc", "allowed_user_id": "42", "allow_provider_setup": true}},
  "modules": {
    "external_agents": {"codex-app": {"enabled": true, "path": "/usr/bin/codex"}},
    "tts": {"enabled": true, "provider_id": "piper", "providers": {
      "piper": {"voice_id": "ru_RU-ruslan-medium", "runtime_mode": "always_running", "binary_path": "piper"},
      "supertonic": {"voice_id": "M1", "language": "auto", "runtime_mode": "per_task", "binary_path": "supertonic", "endpoint": "http://127.0.0.1:7788"}}},
    "stt": {"enabled": true, "provider_id": "whispercpp", "providers": {
      "whispercpp": {"model_id": "small", "language": "auto", "runtime_mode": "per_task", "binary_path": "whisper-cli", "threads": 4}}},
    "realtime_voice": {"enabled": true, "provider_id": "openai_realtime", "providers": {
      "openai_realtime": {"api_key": "sk-rt-1234", "model_id": "gpt-realtime-2.1", "voice_id": "cedar", "language": "ru-RU", "runtime_mode": "per_task"}}},
    "telephony": {"enabled": true, "gateway_url": "http://127.0.0.1:8090", "gateway_token": "gw-1234", "default_profile": "main", "phone_prompt": "Be polite."},
    "browser": {"enabled": true, "provider_id": "playwright", "provider_config": {"runtime_mode": "per_task", "browser_path": "/opt/chrome"}},
    "web_search": {"provider": "tavily", "tavily_key": "tvly-1234"},
    "mcp": {"enabled": true, "servers": [{"id": "files", "name": "Files", "enabled": true, "transport": "stdio", "command": "mcp-files", "args": ["--root", "/srv"], "tool_prefix": "files", "timeout_seconds": 30}]},
    "skills": {"auto_invoke": false, "trust_policy": "ask"}
  }
}
`

type effectiveConfig struct {
	Active    string
	Assistant [2]string
	Providers []ProviderSetupItem
	Runtime   []ProviderConfig
	Voice     []VoiceModuleDescriptor
	Browser   BrowserModuleDescriptor
	Daemon    DaemonConfig
	Clients   ClientsConfig
	Modules   ModulesConfig
}

func effectiveView(cfg Config) effectiveConfig {
	view := effectiveConfig{
		Active:    cfg.ActiveProviderID,
		Assistant: [2]string{cfg.Assistant.NameOrDefault(), cfg.Assistant.CustomInstructions},
		Providers: ProviderItems(cfg),
		Voice:     VoiceModuleDescriptors(cfg.Modules),
		Browser:   BrowserModuleFromConfig(cfg.Modules),
		Daemon:    cfg.Daemon,
		Clients:   cfg.Clients,
		Modules:   cfg.Modules,
	}
	for _, provider := range cfg.Providers {
		runtime, _ := provider.Runtime()
		view.Runtime = append(view.Runtime, runtime)
	}
	view.Modules.TextToSpeech, view.Modules.SpeechToText, view.Modules.Browser = VoiceModuleConfig{}, VoiceModuleConfig{}, BrowserConfig{}
	return view
}

func TestOldSetupFileRoundTripsWithoutLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	if err := os.WriteFile(path, []byte(oldSetupFile), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(path)
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if before, after := effectiveView(loaded), effectiveView(reloaded); !reflect.DeepEqual(before, after) {
		t.Fatalf("effective config changed on save:\nbefore %+v\nafter  %+v", before, after)
	}

	view := effectiveView(reloaded)
	byID := map[string]ProviderConfig{}
	for _, provider := range view.Runtime {
		byID[provider.ID] = provider
	}
	if p := byID["openai"]; p.APIKey != "sk-openai-1234" || p.BaseURL != "https://proxy.example/v1" || p.Model != "gpt-5.4" || p.ReasoningEffort != "high" || p.Name != "OpenAI" {
		t.Fatalf("openai = %+v", p)
	}
	if p := byID["anthropic"]; p.ContextWindow != 150000 || p.MaxOutputTokens != 8000 || p.APIKey != "sk-ant-1234" {
		t.Fatalf("anthropic = %+v", p)
	}
	if p := byID["local-ai"]; p.Name != "Local AI" || p.BaseURL != "http://127.0.0.1:8000/v1" || p.ToolUseMode != "disabled" || p.APIKey != "sk-local-1234" {
		t.Fatalf("custom provider = %+v", p)
	}
	if view.Daemon.Budgets.User.Steps != 500 || view.Daemon.Budgets.Automation.Tokens != 200000 || view.Daemon.CompactModel.Model != "gpt-5.4-mini" || view.Daemon.BackgroundAgents != 2 || view.Daemon.APIToken != "token-1234" || !view.Daemon.AutostartOnBoot {
		t.Fatalf("daemon = %+v", view.Daemon)
	}
	if tts := view.Voice[0].Config; tts.VoiceID != "ru_RU-ruslan-medium" || tts.RuntimeMode != "always_running" || tts.BinaryPath != "piper" {
		t.Fatalf("tts = %+v", tts)
	}
	if stt := view.Voice[1].Config; stt.ModelID != "small" || stt.Threads != 4 || stt.BinaryPath != "whisper-cli" {
		t.Fatalf("stt = %+v", stt)
	}
	if rt := view.Modules.RealtimeVoice.Providers["openai_realtime"]; rt.APIKey != "sk-rt-1234" || rt.VoiceID != "cedar" || rt.Language != "ru-RU" || rt.ModelID != "gpt-realtime-2.1" {
		t.Fatalf("realtime = %+v", rt)
	}
	if view.Browser.Config.BrowserPath != "/opt/chrome" || view.Browser.Config.RuntimeMode != "per_task" || view.Browser.ProviderID != BrowserProviderPlaywright {
		t.Fatalf("browser = %+v", view.Browser)
	}
	if view.Modules.WebSearch.TavilyKey != "tvly-1234" || view.Modules.Telephony.GatewayToken != "gw-1234" || len(view.Modules.MCP.Servers) != 1 || view.Modules.ExternalAgents["codex-app"].Path != "/usr/bin/codex" || view.Modules.Skills.IsAutoInvoke() {
		t.Fatalf("modules = %+v", view.Modules)
	}
	if !view.Clients.Telegram.AllowProviderSetup || view.Clients.Telegram.AllowedUserID != "42" || view.Assistant != [2]string{"matrixclaw", "Be brief."} {
		t.Fatalf("clients %+v assistant %v", view.Clients, view.Assistant)
	}

	text := string(saved)
	for _, kept := range []string{`"https://proxy.example/v1"`, `"gpt-5.4"`, `"ru_RU-ruslan-medium"`, `"always_running"`, `"small"`, `"cedar"`, `"/opt/chrome"`, `"Local AI"`} {
		if !strings.Contains(text, kept) {
			t.Errorf("saved file lost the user value %s", kept)
		}
	}
	for _, dropped := range []string{`"catalog_id"`, `"name": "OpenAI"`, `"OPENAI_API_KEY"`, `"https://api.anthropic.com/v1"`, `"binary_path": "piper"`, `"whisper-cli"`, `"per_task"`, `"provider_id": "playwright"`, `"supertonic"`, `"name": "matrixclaw"`} {
		if strings.Contains(text, dropped) {
			t.Errorf("saved file still repeats the default %s", dropped)
		}
	}
}
