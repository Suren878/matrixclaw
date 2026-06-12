package controlplane

import "strings"

func PickerPageCommand(kind PickerKind, contextID string) string {
	contextID = strings.TrimSpace(contextID)
	switch kind {
	case PickerCommandMenu:
		return helpCommand()
	case PickerSessions:
		return sessionsCommand()
	case PickerSessionRuntime:
		return newSessionCommand()
	case PickerSessionActions:
		if contextID == "" {
			return sessionsCommand()
		}
		return sessionMenuCommand(contextID)
	case PickerSessionModels:
		if contextID == "" {
			return sessionsCommand()
		}
		return sessionMenuCommand(contextID)
	case PickerProvider:
		return providerCommand()
	case PickerProviderCustom:
		return customProviderCommand()
	case PickerProviderActions:
		if contextID == "" {
			return providerCommand()
		}
		return providerCommand(contextID)
	case PickerPermissions:
		return permissionsCommand()
	case PickerContext:
		return contextCommand()
	case PickerModules:
		return modulesCommand()
	case PickerTextToSpeech:
		return textToSpeechCommand()
	case PickerSpeechToText:
		return speechToTextCommand()
	case PickerRealtimeVoice:
		return realtimeVoiceCommand()
	case PickerVoiceProvider:
		if contextID == "" {
			return modulesCommand()
		}
		return voiceModuleCommand(contextID, "provider")
	case PickerExternalAgents:
		return externalAgentsCommand()
	case PickerExternalAgent:
		if contextID == "" {
			return externalAgentsCommand()
		}
		return externalAgentCommand(contextID)
	case PickerStorage:
		return storageCommand()
	case PickerStorageFiles:
		return storageFilesCommand()
	case PickerStorageFile:
		if contextID == "" {
			return storageFilesCommand()
		}
		return storageFileCommand(contextID)
	case PickerStorageTemp:
		return storageTempCommand()
	case PickerStorageCleanup:
		return storageTempCleanupSettingsCommand()
	case PickerStorageTempFile:
		if contextID == "" {
			return storageTempCommand()
		}
		return storageTempFileCommand(contextID)
	case PickerSkills:
		return skillsCommand()
	case PickerSkillsSection:
		section := contextID
		if section == "" {
			section = "library"
		}
		return skillsCommand(section)
	case PickerSkill:
		section, skillID := splitSkillPickerContext(contextID)
		if skillID == "" {
			return skillsCommand()
		}
		return skillsCommand(section, skillID)
	case PickerSessionSkills:
		return sessionSkillsCommand()
	case PickerSessionSkill:
		if contextID == "" {
			return sessionSkillsCommand()
		}
		return sessionSkillCommand(contextID)
	case PickerMCP:
		return mcpCommand()
	case PickerBrowser:
		return browserCommand()
	case PickerMCPServer:
		if contextID == "" {
			return mcpCommand()
		}
		return mcpServerCommand(contextID)
	case PickerTasks:
		return tasksCommand()
	case PickerTaskActions:
		if contextID == "" {
			return tasksCommand()
		}
		return taskMenuCommand(contextID)
	case PickerTaskArchive:
		return tasksArchiveCommand()
	case PickerServer:
		return serverCommand()
	case PickerWebSearch:
		return webSearchCommand()
	case PickerWebSearchProvider:
		if contextID == "" {
			return webSearchCommand()
		}
		return webSearchCommand(contextID)
	default:
		return ""
	}
}
