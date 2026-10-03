package controlplane

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/modules/telephony"
	"github.com/Suren878/matrixclaw/internal/modules/voice"
	"github.com/Suren878/matrixclaw/internal/modules/web"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestModulesListsTheScreensWithTheirState(t *testing.T) {
	daemon, _ := newModulesDaemon(t, web.New(), telephony.New())
	result := daemon.run("/modules")

	if ids := pickerItemIDs(result); !slices.Equal(ids, []string{"agents", "web_search", "telephony"}) {
		t.Fatalf("modules = %v", ids)
	}
	if web := result.Picker.Items[1]; web.Info != "DuckDuckGo" || web.Command != "/modules web_search" {
		t.Fatalf("web search row = %+v", web)
	}
}

func TestChoosingAProviderThatNeedsAKeyAsksForIt(t *testing.T) {
	daemon, service := newModulesDaemon(t, web.New())

	page := daemon.run("/modules web_search")
	if page.Picker == nil || page.Picker.Items[0].Info != "DuckDuckGo" || page.Picker.Back != "/modules" {
		t.Fatalf("page = %+v", page.Picker)
	}
	choice := daemon.run(page.Picker.Items[0].Command)
	if choice.Picker == nil || !choice.Picker.Popup || len(choice.Picker.Items) != 4 || !choice.Picker.Items[0].Selected {
		t.Fatalf("choice = %+v", choice.Picker)
	}

	asked := daemon.run(choice.Picker.Items[1].Command)
	if asked.Prompt == nil || !asked.Prompt.Sensitive || !strings.Contains(asked.Prompt.Title, "Tavily needs its API key") {
		t.Fatalf("choosing Tavily = %+v", asked)
	}
	saved := daemon.run(asked.Prompt.SubmitCommandPrefix + "tvly-secret-1234")
	if saved.Picker == nil || saved.Picker.Items[0].Info != "Tavily" || saved.Picker.Items[1].Info != "****1234" {
		t.Fatalf("after the key = %+v", saved.Picker)
	}
	for _, text := range resultStrings(saved) {
		if strings.Contains(text, "secret") {
			t.Fatalf("the key reached the client: %q", text)
		}
	}
	cfg, _ := service.Load()
	if cfg.Modules.WebSearch.Provider != setup.WebSearchProviderTavily || cfg.Modules.WebSearch.TavilyKey != "tvly-secret-1234" {
		t.Fatalf("web search = %+v", cfg.Modules.WebSearch)
	}

	cleared := daemon.run(asked.Prompt.SubmitCommandPrefix + "-")
	if cleared.Picker == nil || cleared.Picker.Items[0].Info != "DuckDuckGo" {
		t.Fatalf("clearing the active key = %+v", cleared.Picker)
	}
}

func TestTelephonySettingsEditAndValidate(t *testing.T) {
	daemon, service := newModulesDaemon(t, telephony.New())

	page := daemon.run("/modules telephony")
	if enabled := page.Picker.Items[0]; !enabled.Disabled || enabled.Info != "Set the gateway URL first" {
		t.Fatalf("enabled row without a gateway = %+v", enabled)
	}
	prompt := daemon.run("/modules telephony open gateway_url")
	if prompt.Prompt == nil || prompt.Prompt.Sensitive || prompt.Prompt.CancelCommand != "/modules telephony" {
		t.Fatalf("gateway prompt = %+v", prompt)
	}
	if _, err := daemon.dispatcher(core.RoleOwner).Handle(t.Context(), prompt.Prompt.SubmitCommandPrefix+"not a url"); err == nil {
		t.Fatal("a bad gateway URL was saved")
	}
	daemon.run(prompt.Prompt.SubmitCommandPrefix + "http://127.0.0.1:1")
	toggle := daemon.run("/modules telephony open enabled")
	if toggle.Picker == nil || len(toggle.Picker.Items) != 2 {
		t.Fatalf("toggle = %+v", toggle)
	}
	daemon.run(toggle.Picker.Items[0].Command)
	if cfg, _ := service.Load(); !cfg.Modules.Telephony.Enabled || cfg.Modules.Telephony.GatewayURL != "http://127.0.0.1:1" {
		t.Fatalf("telephony = %+v", cfg.Modules.Telephony)
	}
	status := daemon.run("/modules telephony open status")
	if status.Info == nil || status.Info.Rows[0].Value != "Gateway unreachable" {
		t.Fatalf("status = %+v", status.Info)
	}
}

func TestNonOwnersSeeModuleSettingsReadOnly(t *testing.T) {
	daemon, service := newModulesDaemon(t, web.New())

	list := daemon.runAs(core.RoleMember, "/modules")
	if agents := list.Picker.Items[0]; agents.ID != "agents" || agents.Command != "" || !agents.Disabled {
		t.Fatalf("member agents row = %+v", agents)
	}
	if mcp := daemon.runAs(core.RoleGuest, "/modules mcp add"); mcp.Text != ownerOnlySettings {
		t.Fatalf("guest MCP = %+v", mcp)
	}

	page := daemon.runAs(core.RoleMember, "/modules web_search")
	for _, item := range page.Picker.Items {
		if item.Command != "" || !item.Disabled {
			t.Fatalf("member row = %+v", item)
		}
	}
	if _, err := daemon.dispatcher(core.RoleMember).Handle(t.Context(), "/modules web_search set searxng_url http://searx"); err == nil {
		t.Fatal("the daemon took a member's setting")
	}
	if cfg, _ := service.Load(); cfg.Modules.WebSearch.BaseURL != "" {
		t.Fatalf("web search = %+v", cfg.Modules.WebSearch)
	}
}

func TestGroupedChoicesArePickedGroupFirst(t *testing.T) {
	runtime := localruntime.New(filepath.Join(t.TempDir(), "state"))
	runtime.Offline = true
	daemon, _ := newModulesDaemon(t, voice.New(setup.VoiceModuleTTS, runtime))

	page := daemon.run("/modules tts open piper")
	if page.Picker == nil || page.Picker.Title != "Piper" || page.Picker.Back != "/modules tts" {
		t.Fatalf("piper page = %+v", page.Picker)
	}
	groups := daemon.run("/modules tts open piper/voices/add")
	if ids := pickerItemIDs(groups); !slices.Equal(ids, []string{"English", "Russian"}) || groups.Picker.Back != "/modules tts open piper/voices" {
		t.Fatalf("groups = %+v", groups.Picker)
	}
	voices := daemon.run(groups.Picker.Items[1].Command)
	if ids := pickerItemIDs(voices); !slices.Equal(ids, []string{"ru_RU-ruslan-medium"}) || voices.Picker.Title != "Add Voice: Russian" {
		t.Fatalf("Russian voices = %+v", voices.Picker)
	}
	if command := voices.Picker.Items[0].Command; command != "/modules tts set piper/voices/add ru_RU-ruslan-medium" {
		t.Fatalf("voice command = %q", command)
	}
	asked := daemon.run("/modules tts open provider")
	if asked.Picker == nil || asked.Picker.Items[1].Command != "/modules tts confirm provider piper" {
		t.Fatalf("provider choice = %+v", asked.Picker)
	}
	if confirm := daemon.run(asked.Picker.Items[1].Command); confirm.Confirm == nil || confirm.Confirm.Message != "Download the Piper engine?" || confirm.Confirm.ConfirmCommand != "/modules tts set provider piper" {
		t.Fatalf("confirm = %+v", confirm)
	}
}
