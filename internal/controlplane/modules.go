package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleModules(ctx context.Context, externalKey string, args string) (Result, error) {
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
	case "web":
		return d.handleWebSearch(ctx, rest)
	case "skills":
		return d.handleSkillsForExternal(ctx, externalKey, rest)
	case "mcp":
		return d.handleMCP(ctx, rest)
	default:
		return d.modulesPicker(ctx)
	}
}

func (d *Dispatcher) modulesPicker(ctx context.Context) (Result, error) {
	ttsInfo, sttInfo, webInfo, skillsInfo, mcpInfo := "", "", "", "", ""
	if d.voiceModules != nil {
		if module, err := d.voiceModule(ctx, setup.VoiceModuleTTS); err == nil {
			ttsInfo = voiceModuleListInfo(module)
		}
		if module, err := d.voiceModule(ctx, setup.VoiceModuleSTT); err == nil {
			sttInfo = voiceModuleListInfo(module)
		}
	}
	if d.webSearch != nil {
		if resp, err := d.webSearch.GetWebSearchConfig(ctx); err == nil {
			webInfo = setup.WebSearchConfigStatus(resp.Config)
		}
	}
	if d.skills != nil {
		if items, err := d.skills.ListSkills(ctx, skillsLibrarySearchOptions()); err == nil {
			skillsInfo = skillsModuleInfo(items)
		}
	}
	if d.mcp != nil {
		if resp, err := d.mcp.MCPConfig(ctx); err == nil {
			mcpInfo = resp.Status
		}
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerModules, "Modules").
			Row("agents", "External Agents", "Codex", externalAgentsCommand()).
			Row("skills", "Skills", skillsInfo, skillsCommand()).
			Row("mcp", "MCP", mcpInfo, mcpCommand()).
			Row("tts", "Text to Speech", ttsInfo, textToSpeechCommand()).
			Row("stt", "Speech to Text", sttInfo, speechToTextCommand()).
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
