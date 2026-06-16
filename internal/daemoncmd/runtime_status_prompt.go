package daemoncmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type setupRuntimeStatusContext struct {
	setup   *setup.Service
	runtime *localruntime.Runtime
}

func (p *setupRuntimeStatusContext) RuntimeStatusPromptContext(_ context.Context, req core.RuntimeStatusContextRequest) string {
	if p == nil || p.setup == nil {
		return ""
	}
	cfg, err := p.setup.Load()
	if err != nil {
		return ""
	}
	runtime := p.runtime
	if runtime == nil {
		runtime = localruntime.New("")
	}
	toolSet := toolIDSet(req.ToolIDs)
	lines := []string{"Current runtime status (fresh for this request):"}
	lines = append(lines, browserStatusLine(runtime.DecorateBrowserModule(setup.BrowserModuleFromConfig(cfg.Modules)), toolSet))
	lines = append(lines, webSearchStatusLine(cfg.Modules.WebSearch, toolSet))
	for _, module := range runtime.DecorateVoiceModules(setup.VoiceModuleDescriptors(cfg.Modules)) {
		lines = append(lines, voiceStatusLine(module, toolSet))
	}
	lines = append(lines, mcpStatusLine(cfg.Modules.MCP, toolSet))
	lines = append(lines, skillsStatusLine(cfg.Modules.Skills, toolSet))
	lines = append(lines, externalAgentsStatusLine(cfg.Modules.ExternalAgents))
	return strings.Join(lines, "\n")
}

func browserStatusLine(module setup.BrowserModuleDescriptor, tools map[string]struct{}) string {
	provider := selectedBrowserStatusProvider(module)
	toolAvailable := toolPrefixAvailable(tools, "mcp_browser_")
	var toolState string
	switch {
	case !module.Enabled:
		toolState = "unavailable"
	case !provider.RuntimeInstalled || !provider.BrowserInstalled:
		toolState = "unavailable"
	case toolAvailable:
		toolState = "available"
	default:
		toolState = "unavailable_restart_required"
	}
	return fmt.Sprintf("browser: enabled=%t; provider=%s; mode=%s; runtime_installed=%t; browser_installed=%t; browser_tools=%s",
		module.Enabled,
		firstNonEmpty(module.ProviderName, provider.Name, module.ProviderID),
		firstNonEmpty(provider.Config.RuntimeMode, module.Config.RuntimeMode, "per_task"),
		provider.RuntimeInstalled,
		provider.BrowserInstalled,
		toolState,
	)
}

func selectedBrowserStatusProvider(module setup.BrowserModuleDescriptor) setup.BrowserProviderOption {
	for _, provider := range module.Providers {
		if provider.ID == module.ProviderID {
			return provider
		}
	}
	return setup.BrowserProviderOption{ID: module.ProviderID, Name: module.ProviderName, Config: module.Config}
}

func webSearchStatusLine(cfg setup.WebSearchConfig, tools map[string]struct{}) string {
	available := []string{}
	for _, id := range []string{"web_search", "web_fetch"} {
		if _, ok := tools[id]; ok {
			available = append(available, id)
		}
	}
	if len(available) == 0 {
		available = append(available, "unavailable")
	}
	return fmt.Sprintf("web_search: provider=%s; tools=%s", setup.WebSearchConfigStatus(cfg), strings.Join(available, ","))
}

func voiceStatusLine(module setup.VoiceModuleDescriptor, tools map[string]struct{}) string {
	provider := selectedVoiceStatusProvider(module)
	toolState := "unavailable"
	if module.ID == setup.VoiceModuleTTS {
		if _, ok := tools["text_to_speech"]; ok {
			toolState = "available"
		}
	}
	return fmt.Sprintf("%s: enabled=%t; provider=%s; mode=%s; runtime_state=%s; installed_models=%d; tool=%s",
		module.ID,
		module.Enabled,
		firstNonEmpty(module.ProviderName, provider.Name, module.ProviderID),
		firstNonEmpty(provider.Config.RuntimeMode, module.Config.RuntimeMode, "per_task"),
		firstNonEmpty(provider.RuntimeState, "unknown"),
		installedVoiceModelCount(provider.Models),
		toolState,
	)
}

func selectedVoiceStatusProvider(module setup.VoiceModuleDescriptor) setup.VoiceProviderOption {
	for _, provider := range module.Providers {
		if provider.ID == module.ProviderID {
			return provider
		}
	}
	return setup.VoiceProviderOption{ID: module.ProviderID, Name: module.ProviderName, Config: module.Config}
}

func installedVoiceModelCount(models []setup.VoiceModelOption) int {
	count := 0
	for _, model := range models {
		if model.Installed {
			count++
		}
	}
	return count
}

func mcpStatusLine(cfg setup.MCPConfig, tools map[string]struct{}) string {
	mcpTools := 0
	for id := range tools {
		if strings.HasPrefix(id, "mcp_") {
			mcpTools++
		}
	}
	return fmt.Sprintf("mcp: %s; active_tools=%d", setup.MCPConfigStatus(cfg), mcpTools)
}

func skillsStatusLine(cfg setup.SkillsConfig, tools map[string]struct{}) string {
	toolCount := 0
	for _, id := range []string{"skill_search", "skill_use", "skill_manage"} {
		if _, ok := tools[id]; ok {
			toolCount++
		}
	}
	return fmt.Sprintf("skills: enabled=%t; auto_invoke=%t; trust_policy=%s; tools=%d/3", cfg.Enabled, cfg.AutoInvoke, firstNonEmpty(cfg.TrustPolicy, "quarantine"), toolCount)
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

func toolIDSet(ids []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func toolPrefixAvailable(tools map[string]struct{}, prefix string) bool {
	for id := range tools {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}
