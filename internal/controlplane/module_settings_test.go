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
	if web := screenOf[PickerData](t, result).Items[1]; web.Info != "DuckDuckGo" || web.Command != "/modules settings web_search" {
		t.Fatalf("web search row = %+v", web)
	}
}

func TestChoosingAProviderThatNeedsAKeyAsksForIt(t *testing.T) {
	daemon, service := newModulesDaemon(t, web.New())

	page := daemon.run("/modules web_search")
	if screenOf[PickerData](t, page).Items[0].Info != "DuckDuckGo" || screenOf[PickerData](t, page).Back != "/modules" {
		t.Fatalf("page = %+v", screenOf[PickerData](t, page))
	}
	choice := daemon.run(screenOf[PickerData](t, page).Items[0].Command)
	if !isScreen[PickerData](choice) || !screenOf[PickerData](t, choice).Popup || len(screenOf[PickerData](t, choice).Items) != 4 || !screenOf[PickerData](t, choice).Items[0].Selected {
		t.Fatalf("choice = %+v", screenOf[PickerData](t, choice))
	}
	// Clients page long choices by showing the picker again.
	if again := daemon.run(screenOf[PickerData](t, choice).Command); !isScreen[PickerData](again) || len(screenOf[PickerData](t, again).Items) != 4 {
		t.Fatalf("choice command %q shows %+v", screenOf[PickerData](t, choice).Command, again)
	}

	asked := daemon.run(screenOf[PickerData](t, choice).Items[1].Command)
	if !isScreen[PromptData](asked) || !screenOf[PromptData](t, asked).Sensitive || !strings.Contains(screenOf[PromptData](t, asked).Title, "Tavily needs its API key") {
		t.Fatalf("choosing Tavily = %+v", asked)
	}
	saved := daemon.answer(asked, "tvly-secret-1234")
	if screenOf[PickerData](t, saved).Items[0].Info != "Tavily" || screenOf[PickerData](t, saved).Items[1].Info != "****1234" {
		t.Fatalf("after the key = %+v", screenOf[PickerData](t, saved))
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

	cleared := daemon.answer(asked, "-")
	if screenOf[PickerData](t, cleared).Items[0].Info != "DuckDuckGo" {
		t.Fatalf("clearing the active key = %+v", screenOf[PickerData](t, cleared))
	}
}

func TestTelephonySettingsEditAndValidate(t *testing.T) {
	daemon, service := newModulesDaemon(t, telephony.New())

	page := daemon.run("/modules telephony")
	if enabled := screenOf[PickerData](t, page).Items[0]; !enabled.Disabled || enabled.Info != "Set the gateway URL first" {
		t.Fatalf("enabled row without a gateway = %+v", enabled)
	}
	prompt := daemon.run("/modules open telephony gateway_url")
	if screenOf[PromptData](t, prompt).Sensitive || screenOf[PromptData](t, prompt).CancelCommand != "/modules settings telephony" {
		t.Fatalf("gateway prompt = %+v", prompt)
	}
	if _, err := daemon.dispatcher(core.RoleOwner).Answer(t.Context(), screenOf[PromptData](t, prompt).Submit, "not a url"); err == nil {
		t.Fatal("a bad gateway URL was saved")
	}
	daemon.answer(prompt, "http://127.0.0.1:1")
	toggle := daemon.run("/modules open telephony enabled")
	if !isScreen[PickerData](toggle) || len(screenOf[PickerData](t, toggle).Items) != 2 {
		t.Fatalf("toggle = %+v", toggle)
	}
	daemon.run(screenOf[PickerData](t, toggle).Items[0].Command)
	if cfg, _ := service.Load(); !cfg.Modules.Telephony.Enabled || cfg.Modules.Telephony.GatewayURL != "http://127.0.0.1:1" {
		t.Fatalf("telephony = %+v", cfg.Modules.Telephony)
	}
	status := daemon.run("/modules open telephony status")
	if screenOf[InfoData](t, status).Rows[0].Value != "Gateway unreachable" {
		t.Fatalf("status = %+v", screenOf[InfoData](t, status))
	}
}

func TestNonOwnersSeeModuleSettingsReadOnly(t *testing.T) {
	daemon, service := newModulesDaemon(t, web.New())

	list := daemon.runAs(core.RoleMember, "/modules")
	if agents := screenOf[PickerData](t, list).Items[0]; agents.ID != "agents" || agents.Command != "" || !agents.Disabled {
		t.Fatalf("member agents row = %+v", agents)
	}
	if mcp := daemon.runAs(core.RoleGuest, "/modules mcp add"); mcp.Text() != ownerOnlySettings {
		t.Fatalf("guest MCP = %+v", mcp)
	}

	page := daemon.runAs(core.RoleMember, "/modules web_search")
	for _, item := range screenOf[PickerData](t, page).Items {
		if item.Command != "" || !item.Disabled {
			t.Fatalf("member row = %+v", item)
		}
	}
	if _, err := daemon.dispatcher(core.RoleMember).Handle(t.Context(), "/modules set web_search searxng_url http://searx"); err == nil {
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

	page := daemon.run("/modules open tts piper")
	if screenOf[PickerData](t, page).Title != "Piper" || screenOf[PickerData](t, page).Back != "/modules settings tts" {
		t.Fatalf("piper page = %+v", screenOf[PickerData](t, page))
	}
	groups := daemon.run("/modules open tts piper/voices/add")
	if ids := pickerItemIDs(groups); !slices.Equal(ids, []string{"English", "Russian"}) || screenOf[PickerData](t, groups).Back != "/modules open tts piper/voices" {
		t.Fatalf("groups = %+v", screenOf[PickerData](t, groups))
	}
	voices := daemon.run(screenOf[PickerData](t, groups).Items[1].Command)
	if ids := pickerItemIDs(voices); !slices.Equal(ids, []string{"ru_RU-ruslan-medium"}) || screenOf[PickerData](t, voices).Title != "Add Voice: Russian" {
		t.Fatalf("Russian voices = %+v", screenOf[PickerData](t, voices))
	}
	if command := screenOf[PickerData](t, voices).Items[0].Command; command != "/modules set tts piper/voices/add ru_RU-ruslan-medium" {
		t.Fatalf("voice command = %q", command)
	}
	asked := daemon.run("/modules open tts provider")
	if screenOf[PickerData](t, asked).Items[1].Command != "/modules confirm tts provider piper" {
		t.Fatalf("provider choice = %+v", screenOf[PickerData](t, asked))
	}
	if confirm := daemon.run(screenOf[PickerData](t, asked).Items[1].Command); screenOf[ConfirmData](t, confirm).Message != "Download the Piper engine?" || screenOf[ConfirmData](t, confirm).ConfirmCommand != "/modules set tts provider piper" {
		t.Fatalf("confirm = %+v", confirm)
	}
}
