package controlplane

func modulesCommand(parts ...string) string {
	values := append([]string{"modules"}, parts...)
	return controlplaneCommand(values...)
}

func storageCommand(parts ...string) string {
	values := append([]string{"storage"}, parts...)
	return modulesCommand(values...)
}

func voiceModuleCommand(moduleID string, parts ...string) string {
	values := append([]string{moduleID}, parts...)
	return modulesCommand(values...)
}

func voiceModuleCommandPrefix(moduleID string, parts ...string) string {
	return voiceModuleCommand(moduleID, parts...) + " "
}

func textToSpeechCommand(parts ...string) string {
	return voiceModuleCommand("tts", parts...)
}

func speechToTextCommand(parts ...string) string {
	return voiceModuleCommand("stt", parts...)
}

func storageCommandPrefix(parts ...string) string {
	return storageCommand(parts...) + " "
}

func storageImportCommand() string {
	return storageCommand("import")
}

func storageImportCommandPrefix() string {
	return storageCommandPrefix("import")
}

func storageFilesCommand() string {
	return storageCommand("files")
}

func storageFileCommand(storagePath string) string {
	return storageCommand("file", storagePath)
}

func storageReadCommand(storagePath string) string {
	return storageCommand("read", storagePath)
}

func storageDeleteCommand(storagePath string) string {
	return storageCommand("delete", storagePath)
}

func storageDeleteConfirmCommand(storagePath string) string {
	return storageCommand("delete-confirm", storagePath)
}

func storageClearCommand() string {
	return storageCommand("clear")
}

func storageClearConfirmCommand() string {
	return storageCommand("clear-confirm")
}

func storageTempCommand(parts ...string) string {
	values := append([]string{"temp"}, parts...)
	return storageCommand(values...)
}

func storageTempFileCommand(tempPath string) string {
	return storageCommand("temp-file", tempPath)
}

func storageTempPromoteCommand(tempPath string) string {
	return storageCommand("temp-promote", tempPath)
}

func storageTempDeleteCommand(tempPath string) string {
	return storageCommand("temp-delete", tempPath)
}

func storageTempDeleteConfirmCommand(tempPath string) string {
	return storageCommand("temp-delete-confirm", tempPath)
}

func storageTempCleanupCommand() string {
	return storageCommand("temp-cleanup")
}

func storageTempCleanupConfirmCommand() string {
	return storageCommand("temp-cleanup-confirm")
}

func storageTempCleanupModeCommand() string {
	return storageCommand("temp-cleanup-mode")
}

func storageTempToggleCommand(value string) string {
	return storageCommand("temp-toggle", value)
}

func storageTempDaysCommand() string {
	return storageCommand("temp-days")
}

func storageTempDaysCommandPrefix() string {
	return storageCommandPrefix("temp-days")
}

func storageTempMaxCommand() string {
	return storageCommand("temp-max")
}

func storageTempMaxCommandPrefix() string {
	return storageCommandPrefix("temp-max")
}

func externalAgentsCommand(parts ...string) string {
	values := append([]string{"agents"}, parts...)
	return modulesCommand(values...)
}

func externalAgentCommand(agentID string, parts ...string) string {
	values := append([]string{agentID}, parts...)
	return externalAgentsCommand(values...)
}

func externalAgentPathCommandPrefix(agentID string) string {
	return externalAgentCommand(agentID, "path") + " "
}

func externalAgentEnabledCommand(agentID string) string {
	return externalAgentCommand(agentID, "enabled")
}

func externalAgentSetEnabledCommand(agentID string, value string) string {
	return externalAgentCommand(agentID, "set-enabled", value)
}

func externalAgentUpdateEnabledCommand(agentID string, enabled bool) string {
	if enabled {
		return externalAgentsCommand("enable", agentID)
	}
	return externalAgentsCommand("disable", agentID)
}

func externalAgentNewSessionCommand(agentID string) string {
	return sessionNewCommand(agentID)
}
