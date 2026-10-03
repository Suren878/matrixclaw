package controlplane

import (
	"context"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleModules(ctx context.Context, args string) (Result, error) {
	step, rest := firstCommandStep(args)
	if step == "" {
		return d.modulesPicker(ctx)
	}
	switch step {
	case "agents":
		return d.handleExternalAgents(ctx, rest)
	case "storage":
		return d.handleStorage(ctx, rest)
	case "tts":
		return d.handleVoiceModule(ctx, setup.VoiceModuleTTS, rest)
	case "stt":
		return d.handleVoiceModule(ctx, setup.VoiceModuleSTT, rest)
	case "realtime", "realtime_voice", "live-voice", "live_voice":
		return d.handleRealtimeVoiceModule(ctx, rest)
	case "telephony", "calls":
		return d.handleTelephonyModule(ctx, rest)
	case "web":
		return d.handleWebSearch(ctx, rest)
	case "browser":
		return d.handleBrowserModule(ctx, rest)
	case "skills":
		return d.handleSkillsForExternal(ctx, rest)
	case "mcp":
		return d.handleMCP(ctx, rest)
	default:
		return d.modulesPicker(ctx)
	}
}

func (d *Dispatcher) modulesPicker(ctx context.Context) (Result, error) {
	externalAgentsInfo, ttsInfo, sttInfo, realtimeVoiceInfo, telephonyInfo, browserInfo, webInfo, skillsInfo, mcpInfo := "", "", "", "", "", "", "", "", ""
	if agents, err := d.daemon.ListExternalAgents(ctx); err == nil {
		externalAgentsInfo = externalAgentsModuleInfo(agents)
	}
	if module, err := d.voiceModule(ctx, setup.VoiceModuleTTS); err == nil {
		ttsInfo = voiceModuleListInfo(module)
	}
	if module, err := d.voiceModule(ctx, setup.VoiceModuleSTT); err == nil {
		sttInfo = voiceModuleListInfo(module)
	}
	if module, err := d.daemon.RealtimeVoiceModule(ctx); err == nil {
		realtimeVoiceInfo = realtimeVoiceModuleListInfo(module)
	}
	if module, err := d.daemon.TelephonyModule(ctx); err == nil {
		telephonyInfo = telephonyModuleListInfo(module)
	}
	if resp, err := d.daemon.GetWebSearchConfig(ctx); err == nil {
		webInfo = resp.Status
	}
	if module, err := d.daemon.BrowserModule(ctx); err == nil {
		browserInfo = browserModuleListInfo(module)
	}
	if items, err := d.daemon.ListSkills(ctx, skillsLibrarySearchOptions()); err == nil {
		skillsInfo = skillsModuleInfo(items)
	}
	if resp, err := d.daemon.MCPConfig(ctx); err == nil {
		mcpInfo = mcpExternalConfigStatus(resp.Config)
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerModules, "Modules").
			Command(modulesCommand()).
			Row("agents", "External Agents", externalAgentsInfo, externalAgentsCommand()).
			Row("skills", "Skills", skillsInfo, skillsCommand()).
			Row("mcp", "External MCP", mcpInfo, mcpCommand()).
			Row("tts", "Text to Speech", ttsInfo, textToSpeechCommand()).
			Row("stt", "Speech to Text", sttInfo, speechToTextCommand()).
			Row("realtime_voice", "Realtime Voice", realtimeVoiceInfo, realtimeVoiceCommand()).
			Row("telephony", "Telephony", telephonyInfo, telephonyCommand()).
			Row("browser", "Browser", browserInfo, browserCommand()).
			Row("storage", "Storage", "Files", storageCommand()).
			Row("web", "Web Search", webInfo, webSearchCommand()).
			Ptr(),
	}, nil
}

func voiceModuleListInfo(module setup.VoiceModuleDescriptor) string {
	if !module.Enabled {
		return ""
	}
	if module.ID == setup.VoiceModuleSTT && module.Local {
		for _, provider := range module.Providers {
			if provider.ID != module.ProviderID {
				continue
			}
			if model, ok := activeInstalledModel(provider); ok {
				return voiceModelName(provider, model.ID)
			}
			return firstNonEmptyTrimmed(module.Config.ModelID, module.ProviderName)
		}
	}
	return strings.TrimSpace(module.ProviderName)
}

func externalAgentsModuleInfo(agents []core.ExternalAgentDescriptor) string {
	if len(agents) == 0 {
		return ""
	}
	enabled := make([]string, 0, len(agents))
	installed := make([]string, 0, len(agents))
	for _, agent := range agents {
		title := externalAgentTitle(agent)
		if title == "" {
			continue
		}
		if agent.Enabled {
			enabled = append(enabled, title)
		}
		if agent.Installed {
			installed = append(installed, title)
		}
	}
	switch len(enabled) {
	case 1:
		return enabled[0]
	case 2:
		return strings.Join(enabled, ", ")
	default:
		if len(enabled) > 2 {
			return strconv.Itoa(len(enabled)) + " enabled"
		}
	}
	if len(installed) == 1 {
		return "Disabled · " + installed[0]
	}
	if len(installed) > 1 {
		return strconv.Itoa(len(installed)) + " installed · disabled"
	}
	return "Not installed"
}
