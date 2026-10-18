package daemoncmd

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents/claudecode"
	"github.com/Suren878/matrixclaw/internal/modules"
	browsermodule "github.com/Suren878/matrixclaw/internal/modules/browser"
	deliverymodule "github.com/Suren878/matrixclaw/internal/modules/delivery"
	"github.com/Suren878/matrixclaw/internal/modules/geo"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	mcpmodule "github.com/Suren878/matrixclaw/internal/modules/mcp"
	skillsmodule "github.com/Suren878/matrixclaw/internal/modules/skills"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	telephonymodule "github.com/Suren878/matrixclaw/internal/modules/telephony"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	webmodule "github.com/Suren878/matrixclaw/internal/modules/web"
	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// daemonModules are the daemon's modules; the API serves some of them
// directly.
type daemonModules struct {
	set *modules.Set
	api api.Modules
}

type moduleDeps struct {
	app        *core.Core
	automation *automation.Service
	runtime    *localruntime.Runtime
	storage    *localstorage.Module
	skills     *skillsmodule.Module
	realtime   *realtime.Manager
	geo        *geo.OSMService
}

// buildModules builds every module once and the set that applies them.
func buildModules(deps moduleDeps) (daemonModules, error) {
	tts := voicemodule.New(setup.VoiceModuleTTS, deps.runtime)
	stt := voicemodule.New(setup.VoiceModuleSTT, deps.runtime)
	telephony := telephonymodule.New()
	browser := browsermodule.New(deps.runtime)
	mcp := mcpmodule.New(browser.MCPServer)
	app := deps.app
	tools.ProtectCredentials(setup.CredentialPaths, openaicodex.CredentialPaths, claudecode.CredentialPaths)
	base := append(tools.CoreExecutors(),
		automation.NewReminderTool(deps.automation),
		automation.NewScheduledAITaskTool(deps.automation),
	)
	base = append(base, tools.NewShellExecutors(app)...)
	base = append(base, core.TodoToolExecutors(app)...)
	base = append(base, core.AwaitToolExecutors(app)...)
	base = append(base, core.MemoryToolExecutors(app)...)
	base = append(base, core.CheckpointToolExecutors(app)...)
	base = append(base, core.GoalToolExecutors(app)...)
	base = append(base, core.AgentToolExecutors(app)...)
	base = append(base, core.PlanModeToolExecutors(app)...)
	set, err := modules.NewSet(base,
		modules.Static("storage", "Storage", deps.storage.Tools()...),
		modules.Static("delivery", "File delivery", deliverymodule.NewSendFileTool(deps.storage.Store(), app)),
		modules.Static("geo", "Maps", geo.NewOSMGeoExecutors(deps.geo)...),
		webmodule.New(),
		tts,
		stt,
		deps.realtime,
		telephony,
		browser,
		mcp,
		deps.skills,
	)
	if err != nil {
		return daemonModules{}, err
	}
	return daemonModules{set: set, api: api.Modules{Set: set, TTS: tts, STT: stt}}, nil
}

// RuntimeStatusPromptContext is the model's note on module state: modules this
// run can use, those enabled without tools here or not ready, and those off,
// with facts only for enabled ones. It leaves out state text that changes while
// a module runs, so the note stays stable.
func (s *supervisor) RuntimeStatusPromptContext(ctx context.Context, req core.RuntimeStatusContextRequest) string {
	visible := map[string]bool{}
	for _, id := range req.ToolIDs {
		visible[strings.ToLower(strings.TrimSpace(id))] = true
	}
	var usable, idle, unready, off []string
	for _, status := range s.modules.Statuses(ctx) {
		if !status.Enabled {
			off = append(off, status.ID)
			continue
		}
		name := status.ID
		if len(status.Facts) > 0 {
			facts := make([]string, 0, len(status.Facts))
			for _, fact := range status.Facts {
				facts = append(facts, fact.Key+"="+fact.Value)
			}
			name += " (" + strings.Join(facts, ", ") + ")"
		}
		switch {
		case !status.Ready:
			unready = append(unready, name)
		case slices.ContainsFunc(status.Tools, func(id string) bool { return visible[strings.ToLower(id)] }):
			usable = append(usable, name)
		default:
			idle = append(idle, name)
		}
	}
	lines := []string{"Modules (as of this note; a newer note replaces it):"}
	for _, group := range []struct {
		label string
		ids   []string
	}{
		{"ready, with tools in this run", usable},
		{"enabled, no tools in this run", idle},
		{"enabled but not ready", unready},
		{"off (the owner turns them on with /modules)", off},
	} {
		if len(group.ids) > 0 {
			lines = append(lines, "- "+group.label+": "+strings.Join(group.ids, "; "))
		}
	}
	return strings.Join(append(lines, "- "+externalAgentsStatusLine(s.appliedConfig().Modules.ExternalAgents)), "\n")
}

func externalAgentsStatusLine(configs map[string]setup.ExternalAgentConfig) string {
	if len(configs) == 0 {
		return "external_agents: none enabled"
	}
	ids := make([]string, 0, len(configs))
	for id, cfg := range configs {
		state := "disabled"
		if cfg.Enabled {
			state = "enabled"
		}
		ids = append(ids, strings.TrimSpace(id)+"="+state)
	}
	sort.Strings(ids)
	return "external_agents: " + strings.Join(ids, ",")
}
