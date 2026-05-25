package controlplane

import "strings"

func PickerCommandFor(kind PickerKind, contextID string, itemID string) string {
	contextID = strings.TrimSpace(contextID)
	itemID = strings.TrimSpace(itemID)
	switch kind {
	case PickerSessions:
		if itemID == "" {
			return sessionsCommand()
		}
		if itemID == "cancel" {
			return ""
		}
		if itemID == "new" {
			return newSessionCommand()
		}
		return sessionMenuCommand(itemID)
	case PickerSessionRuntime:
		switch itemID {
		case "":
			return newSessionCommand()
		case "matrixclaw":
			return sessionNewCommand("matrixclaw")
		case "codex":
			return sessionNewCommand("codex")
		case "back":
			return sessionsCommand()
		case "cancel":
			return ""
		default:
			return sessionNewCommand(itemID)
		}
	case PickerSessionActions:
		switch itemID {
		case "use":
			if contextID == "" {
				return sessionsCommand()
			}
			return sessionUseCommand(contextID)
		case "rename":
			if contextID == "" {
				return sessionsCommand()
			}
			return sessionRenameCommand(contextID)
		case "delete":
			if contextID == "" {
				return sessionsCommand()
			}
			return sessionDeleteCommand(contextID)
		case "back", "cancel":
			return sessionsCommand()
		default:
			return sessionsCommand()
		}
	case PickerProvider:
		if itemID == "" {
			return providerCommand()
		}
		if itemID == "cancel" {
			return ""
		}
		if itemID == "custom" {
			return customProviderCommand()
		}
		return providerCommand(itemID)
	case PickerProviderCustom:
		switch itemID {
		case "":
			return customProviderCommand()
		case "openai":
			return customProviderCommand("openai")
		case "anthropic":
			return customProviderCommand("anthropic")
		case "back":
			return providerCommand()
		case "cancel":
			return providerCommand()
		default:
			return customProviderCommand()
		}
	case PickerProviderActions:
		if contextID == "" {
			return providerCommand()
		}
		switch itemID {
		case "":
			return providerCommand(contextID)
		case "use":
			return providerUseCommand(contextID)
		case "edit":
			return providerEditCommand(contextID)
		case "delete":
			return customProviderCommand("delete", providerEncodedID(contextID))
		case "back":
			return providerCommand()
		case "cancel":
			return providerCommand()
		default:
			return providerCommand(contextID)
		}
	case PickerPermissions:
		if itemID == "" {
			return permissionsCommand()
		}
		if itemID == "cancel" {
			return ""
		}
		return permissionsCommand(itemID)
	case PickerModules:
		switch itemID {
		case "":
			return modulesCommand()
		case "storage":
			return storageCommand()
		case "tts":
			return textToSpeechCommand()
		case "stt":
			return speechToTextCommand()
		case "agents":
			return externalAgentsCommand()
		case "skills":
			return skillsCommand()
		case "mcp":
			return mcpCommand()
		case "cancel":
			return ""
		default:
			return modulesCommand()
		}
	case PickerTextToSpeech:
		switch itemID {
		case "":
			return textToSpeechCommand()
		case "enabled":
			return textToSpeechCommand("enabled")
		case "provider":
			return textToSpeechCommand("provider")
		case "local", "info", "status":
			return textToSpeechCommand("info")
		case "back":
			return modulesCommand()
		case "cancel":
			return ""
		default:
			return textToSpeechCommand()
		}
	case PickerSpeechToText:
		switch itemID {
		case "":
			return speechToTextCommand()
		case "enabled":
			return speechToTextCommand("enabled")
		case "provider":
			return speechToTextCommand("provider")
		case "local", "info":
			return speechToTextCommand("info")
		case "back":
			return modulesCommand()
		case "cancel":
			return ""
		default:
			return speechToTextCommand()
		}
	case PickerVoiceProvider:
		if contextID == "" {
			return modulesCommand()
		}
		switch itemID {
		case "":
			return voiceModuleCommand(contextID, "provider")
		case "back":
			return voiceModuleCommand(contextID)
		case "cancel":
			return ""
		default:
			return voiceModuleCommand(contextID, "provider", itemID)
		}
	case PickerExternalAgents:
		switch {
		case itemID == "":
			return externalAgentsCommand()
		case itemID == "back":
			return modulesCommand()
		case itemID == "cancel":
			return ""
		default:
			return externalAgentCommand(itemID)
		}
	case PickerExternalAgent:
		if contextID == "" {
			return externalAgentsCommand()
		}
		switch itemID {
		case "path":
			return externalAgentCommand(contextID, "path")
		case "enabled":
			return externalAgentEnabledCommand(contextID)
		case "enable":
			return externalAgentUpdateEnabledCommand(contextID, true)
		case "disable":
			return externalAgentUpdateEnabledCommand(contextID, false)
		case "new":
			return externalAgentNewSessionCommand(contextID)
		case "back":
			return externalAgentsCommand()
		case "cancel":
			return ""
		default:
			return externalAgentCommand(contextID)
		}
	case PickerStorage:
		switch itemID {
		case "":
			return storageCommand()
		case "temp":
			return storageTempCommand()
		case "files":
			return storageFilesCommand()
		case "back":
			return modulesCommand()
		case "cancel":
			return ""
		default:
			return storageCommand()
		}
	case PickerStorageFiles:
		switch {
		case itemID == "":
			return storageFilesCommand()
		case itemID == "clear":
			return storageClearCommand()
		case itemID == "back":
			return storageCommand()
		case itemID == "cancel":
			return ""
		case strings.HasPrefix(itemID, "file:"):
			return storageFileCommand(strings.TrimPrefix(itemID, "file:"))
		default:
			return storageFilesCommand()
		}
	case PickerStorageFile:
		if contextID == "" {
			return storageFilesCommand()
		}
		switch itemID {
		case "read":
			return storageReadCommand(contextID)
		case "delete":
			return storageDeleteCommand(contextID)
		case "back":
			return storageFilesCommand()
		case "cancel":
			return ""
		default:
			return storageFileCommand(contextID)
		}
	case PickerStorageTemp:
		switch {
		case itemID == "":
			return storageTempCommand()
		case itemID == "settings":
			return storageTempCleanupSettingsCommand()
		case itemID == "back":
			return storageCommand()
		case itemID == "cancel":
			return ""
		case strings.HasPrefix(itemID, "temp:"):
			return storageTempFileCommand(strings.TrimPrefix(itemID, "temp:"))
		default:
			return storageTempCommand()
		}
	case PickerStorageCleanup:
		switch itemID {
		case "":
			return storageTempCleanupSettingsCommand()
		case "toggle":
			return storageTempCleanupModeCommand()
		case "days":
			return storageTempDaysCommand()
		case "max":
			return storageTempMaxCommand()
		case "cleanup":
			return storageTempCleanupCommand()
		case "on":
			return storageTempToggleCommand("on")
		case "off":
			return storageTempToggleCommand("off")
		case "back", "cancel":
			return storageTempCommand()
		default:
			return storageTempCleanupSettingsCommand()
		}
	case PickerStorageTempFile:
		if contextID == "" {
			return storageTempCommand()
		}
		switch itemID {
		case "promote":
			return storageTempPromoteCommand(contextID)
		case "delete":
			return storageTempDeleteCommand(contextID)
		case "back":
			return storageTempCommand()
		case "cancel":
			return ""
		default:
			return storageTempFileCommand(contextID)
		}
	case PickerSkills:
		switch {
		case itemID == "":
			return skillsCommand()
		case itemID == "library", itemID == "review", itemID == "installed", itemID == "drafts", itemID == "usage", itemID == "add":
			return skillsCommand(itemID)
		case itemID == "install", itemID == "import":
			return skillsCommand("import")
		case itemID == "create":
			return skillsCommand("create")
		case itemID == "ai-create":
			return skillsCommand("ai-create")
		case itemID == "search":
			return skillSearchCommand()
		case itemID == "back":
			return modulesCommand()
		case itemID == "cancel":
			return ""
		case itemID == "empty":
			return skillsCommand()
		default:
			return skillsCommand(itemID)
		}
	case PickerSkillsSection:
		section := contextID
		if section == "" {
			section = "library"
		}
		switch itemID {
		case "":
			return skillsCommand(section)
		case "back":
			return skillsCommand()
		case "cancel":
			return ""
		case "empty":
			return skillsCommand(section)
		case "manual":
			return skillsCommand("create")
		case "ai":
			return skillsCommand("ai-create")
		case "import":
			return skillsCommand("import")
		default:
			return skillsCommand(section, itemID)
		}
	case PickerSkill:
		section, skillID := splitSkillPickerContext(contextID)
		if skillID == "" {
			return skillsCommand()
		}
		switch itemID {
		case "view":
			return skillsCommand(section, skillID, "view")
		case "trust", "trust-enable", "quarantine", "archive", "restore", "remove", "enabled", "enable", "disable", "pin", "unpin", "keep", "edit", "edit-metadata", "edit-body":
			return skillsCommand(section, skillID, itemID)
		case "back":
			return skillsCommand(section)
		case "cancel":
			return ""
		default:
			return skillsCommand(section, skillID)
		}
	case PickerSessionSkills:
		switch itemID {
		case "":
			return sessionSkillsCommand()
		case "back", "cancel":
			return ""
		case "empty":
			return sessionSkillsCommand()
		default:
			return sessionSkillCommand(itemID)
		}
	case PickerSessionSkill:
		if contextID == "" {
			return sessionSkillsCommand()
		}
		switch itemID {
		case "view", "use", "unload":
			return sessionSkillCommand(contextID, itemID)
		case "back":
			return sessionSkillsCommand()
		case "cancel":
			return ""
		default:
			return sessionSkillCommand(contextID)
		}
	case PickerMCP:
		switch itemID {
		case "":
			return mcpCommand()
		case "enabled":
			return mcpCommand("enabled")
		case "back":
			return modulesCommand()
		case "cancel":
			return ""
		case "empty":
			return mcpCommand()
		default:
			return mcpServerCommand(itemID)
		}
	case PickerMCPServer:
		if contextID == "" {
			return mcpCommand()
		}
		switch itemID {
		case "enabled":
			return mcpServerCommand(contextID, "enabled")
		case "info":
			return mcpServerCommand(contextID, "info")
		case "edit":
			return mcpServerCommand(contextID, "edit")
		case "back":
			return mcpCommand()
		case "cancel":
			return ""
		default:
			return mcpServerCommand(contextID)
		}
	case PickerServer:
		switch itemID {
		case "":
			return serverCommand()
		case "status":
			return statusCommand()
		case "restart":
			return restartCommand()
		case "cancel":
			return ""
		default:
			return serverCommand()
		}
	case PickerTasks:
		if itemID == "" {
			return tasksCommand()
		}
		if itemID == "cancel" {
			return ""
		}
		if itemID == "archive" {
			return tasksArchiveCommand()
		}
		if strings.HasPrefix(itemID, "open:") {
			return taskMenuCommand(strings.TrimPrefix(itemID, "open:"))
		}
		if strings.HasPrefix(itemID, "closed:") {
			return taskMenuCommand(strings.TrimPrefix(itemID, "closed:"))
		}
		return tasksCommand()
	case PickerTaskActions:
		if contextID == "" {
			return tasksCommand()
		}
		switch itemID {
		case "run":
			return taskRunCommand(contextID)
		case "archive":
			return taskCompleteCommand(contextID)
		case "delete":
			return taskDeleteCommand(contextID)
		case "back", "cancel":
			return tasksCommand()
		default:
			return tasksCommand()
		}
	case PickerTaskArchive:
		switch itemID {
		case "":
			return tasksArchiveCommand()
		case "delete_closed":
			return tasksDeleteClosedCommand()
		case "back", "cancel":
			return tasksCommand()
		default:
			if strings.HasPrefix(itemID, "closed:") {
				return taskMenuCommand(strings.TrimPrefix(itemID, "closed:"))
			}
			return tasksArchiveCommand()
		}
	default:
		return ""
	}
}
