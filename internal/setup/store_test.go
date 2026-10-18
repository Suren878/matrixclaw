package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// oldSetupFile is shaped like a file older versions wrote: catalog defaults
// copied into every provider and voice provider, and user choices that differ
// from the defaults.
const oldSetupFile = `{
  "version": 3,
  "active_provider_id": "openai",
  "assistant": {"name": "matrixclaw", "custom_instructions": "Be brief."},
  "providers": [
    {"id": "openai", "catalog_id": "openai", "name": "OpenAI", "type": "openai-compatible", "api_key": "sk-openai-1234", "api_key_env": "OPENAI_API_KEY", "base_url": "https://proxy.example/v1", "model": "gpt-5.4", "reasoning_effort": "high", "tool_use_mode": "native"},
    {"id": "qwen", "name": "Qwen / DashScope", "type": "openai-compatible", "api_key": "sk-qwen-1234", "api_key_env": "DASHSCOPE_API_KEY", "base_url": "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "model": "qwen-plus", "reasoning_effort": "medium"},
    {"id": "anthropic", "name": "Anthropic", "type": "anthropic-compatible", "api_key": "sk-ant-1234", "api_key_env": "ANTHROPIC_API_KEY", "base_url": "https://api.anthropic.com/v1", "model": "claude-sonnet-4-5", "context_window": 150000, "max_output_tokens": 8000},
    {"id": "local-ai", "name": "Local AI", "type": "openai-compatible", "api_key": "sk-local-1234", "api_key_env": "OPENAI_COMPAT_API_KEY", "base_url": "http://127.0.0.1:8000/v1", "model": "qwen3", "tool_use_mode": "disabled"}
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
	Voice     [2]VoiceProviderConfig
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
		Voice:     [2]VoiceProviderConfig{effectiveVoice(VoiceModuleTTS, cfg.Modules.TextToSpeech), effectiveVoice(VoiceModuleSTT, cfg.Modules.SpeechToText)},
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

func effectiveVoice(moduleID string, module VoiceModuleConfig) VoiceProviderConfig {
	return EffectiveVoiceConfig(moduleID, module.ProviderID, module.Providers[module.ProviderID])
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
	if tts := view.Voice[0]; tts.VoiceID != "ru_RU-ruslan-medium" || tts.RuntimeMode != "always_running" || tts.BinaryPath != "piper" {
		t.Fatalf("tts = %+v", tts)
	}
	if stt := view.Voice[1]; stt.ModelID != "small" || stt.Threads != 4 || stt.BinaryPath != "whisper-cli" {
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
	for _, dropped := range []string{`"name": "OpenAI"`, `"OPENAI_API_KEY"`, `"https://api.anthropic.com/v1"`, `"binary_path": "piper"`, `"whisper-cli"`, `"per_task"`, `"provider_id": "playwright"`, `"supertonic"`, `"name": "matrixclaw"`} {
		if strings.Contains(text, dropped) {
			t.Errorf("saved file still repeats the default %s", dropped)
		}
	}
}

func TestVersion4SetupFileFailsNamingTheKey(t *testing.T) {
	for _, tc := range []struct{ name, modules, want string }{
		{"retired key", `{"web_search": {"provider": "tavily", "api_key": "tvly-1"}}`, `unknown keys modules.web_search.api_key`},
		{"browser provider alias", `{"browser": {"provider_id": "local-playwright"}}`, `modules.browser.provider_id is "local-playwright", want one of ["playwright"]`},
		{"runtime mode alias", `{"tts": {"providers": {"piper": {"runtime_mode": "persistent"}}}}`, `modules.tts.providers.piper.runtime_mode is "persistent"`},
		{"external agent alias", `{"external_agents": {"codex": {"enabled": true}}}`, `modules.external_agents key is "codex", want one of ["claude-code" "codex-app"]`},
		{"mcp transport alias", `{"mcp": {"servers": [{"id": "files", "transport": "streamable_http", "endpoint": "http://x"}]}}`, `modules.mcp.servers[0].transport is "streamable_http", want one of ["stdio" "http"]`},
		{"piper language alias", `{"tts": {"providers": {"piper": {"language": "english"}}}}`, `modules.tts.providers.piper.language is "english", want a code such as "en_US"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "setup.json")
			file := `{"version": 4, "daemon": {"http_addr": "127.0.0.1:7777", "db_path": "/tmp/m.db"}, "clients": {"telegram": {"enabled": false}}, "modules": ` + tc.modules + `}`
			if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := NewFileStore(path).Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), path) {
				t.Fatalf("load error = %v, want it to name the file and %s", err, tc.want)
			}
		})
	}
}

// v3SetupFile has every key and alias value v0.2.0 still read.
const v3SetupFile = `{
  "version": 3,
  "completed_at": "2026-09-24T05:08:33Z",
  "active_provider_id": "openai",
  "providers": [{"id": "openai", "catalog_id": "openai", "type": "OpenAI-Compatible", "api_key": "sk-openai-1234", "model": "gpt-5.4", "tool_use_mode": "Native"}],
  "daemon": {"http_addr": "127.0.0.1:7777", "db_path": "/var/lib/matrixclaw.db"},
  "clients": {"terminal": {"enabled": true}, "telegram": {"enabled": false}},
  "modules": {
    "skills": {"trust_policy": "ask", "self_improve": "drafts"},
    "web_search": {"provider": "Tavily", "api_key": "tvly-1234"},
    "browser": {"enabled": true, "provider_id": "local-playwright", "provider_config": {"runtime_mode": "persistent"}},
    "tts": {"enabled": true, "provider_id": "Piper", "providers": {
      "piper": {"language": "english", "runtime_mode": "server"},
      "supertonic": {"language": "EN"},
      "espeak": {"voice_id": "x"}}},
    "stt": {"providers": {"whispercpp": {"runtime_mode": "always"}}},
    "external_agents": {"codex": {"enabled": true, "path": "/usr/bin/codex"}, "Claude-Code": {"enabled": true}},
    "mcp": {"servers": [{"id": "web", "transport": "streamable-http", "endpoint": "http://127.0.0.1:9000"}]}
  }
}
`

func TestVersion3SetupFileIsConvertedOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	if err := os.WriteFile(path, []byte(v3SetupFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewFileStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	modules := cfg.Modules
	piper := modules.TextToSpeech.Providers["piper"]
	if cfg.Version != 4 || cfg.Providers[0].Type != "" || cfg.Providers[0].ToolUseMode != "native" || modules.WebSearch.Provider != WebSearchProviderTavily ||
		modules.WebSearch.TavilyKey != "tvly-1234" || modules.Browser.ProviderConfig.RuntimeMode != "always_running" || modules.TextToSpeech.ProviderID != "piper" ||
		piper.Language != "en_US" || piper.RuntimeMode != "always_running" || modules.SpeechToText.Providers["whispercpp"].RuntimeMode != "always_running" ||
		modules.ExternalAgents["codex-app"].Path != "/usr/bin/codex" || !modules.ExternalAgents["claude-code"].Enabled || modules.MCP.Servers[0].Transport != "http" {
		t.Fatalf("converted config = %+v", cfg)
	}
	backup, err := os.ReadFile(path + ".pre-v4")
	if err != nil || string(backup) != v3SetupFile {
		t.Fatalf("backup = %q, %v", backup, err)
	}
	if info, err := os.Stat(path + ".pre-v4"); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v, %v", info.Mode(), err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"catalog_id", "completed_at", "terminal", "self_improve", "api_key\": \"tvly", "local-playwright", "persistent", "english", "streamable", "espeak", `"codex"`} {
		if strings.Contains(string(saved), gone) {
			t.Errorf("converted file still has %s", gone)
		}
	}
	if err := os.WriteFile(path+".pre-v4", []byte("earlier"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path).Load(); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); string(again) != string(saved) {
		t.Fatal("a version 4 file was rewritten on load")
	}
	if kept, _ := os.ReadFile(path + ".pre-v4"); string(kept) != "earlier" {
		t.Fatal("the existing backup was replaced")
	}
}

func TestVersion3SetupFileThatCannotConvertIsLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	file := strings.Replace(v3SetupFile, `"completed_at"`, `"wizard_state": {}, "completed_at"`, 1)
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path).Load(); err == nil || !strings.Contains(err.Error(), "unknown keys wizard_state") {
		t.Fatalf("load error = %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != file {
		t.Fatal("the file changed")
	}
	if _, err := os.Stat(path + ".pre-v4"); !os.IsNotExist(err) {
		t.Fatalf("backup written: %v", err)
	}
}

func TestDaemonCheckpointsSwitch(t *testing.T) {
	for _, tc := range []struct {
		name, checkpoints string
		on                bool
		wantErr           string
	}{
		{"absent", ``, true, ""},
		{"off", `, "checkpoints": {"enabled": false}`, false, ""},
		{"on", `, "checkpoints": {"enabled": true}`, true, ""},
		{"not a bool", `, "checkpoints": {"enabled": "no"}`, false, "enabled"},
		{"unknown key", `, "checkpoints": {"keep": 5}`, false, "unknown keys daemon.checkpoints.keep"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "setup.json")
			file := `{"version": 4, "daemon": {"http_addr": "127.0.0.1:7777", "db_path": "/tmp/m.db"` + tc.checkpoints + `}, "clients": {"telegram": {"enabled": false}}}`
			if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := NewFileStore(path).Load()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("load error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Daemon.Checkpoints.On(); got != tc.on {
				t.Fatalf("On() = %v, want %v", got, tc.on)
			}
		})
	}
}
