package controlplane

func newSessionCommand(parts ...string) string {
	values := append([]string{"new"}, parts...)
	return controlplaneCommand(values...)
}

func sessionsCommand() string {
	return controlplaneCommand("sessions")
}

func sessionCommand(parts ...string) string {
	values := append([]string{"session"}, parts...)
	return controlplaneCommand(values...)
}

func sessionNewCommand(runtimeID string) string {
	return sessionCommand("new", runtimeID)
}

func sessionMenuCommand(sessionID string) string {
	return sessionCommand("menu", sessionID)
}

func sessionUseCommand(sessionID string) string {
	return sessionCommand("use", sessionID)
}

func sessionRenameCommand(sessionID string) string {
	return sessionCommand("rename", sessionID)
}

func sessionRenameCommandPrefix(sessionID string) string {
	return sessionRenameCommand(sessionID) + " "
}

func sessionDeleteCommand(sessionID string) string {
	return sessionCommand("delete", sessionID)
}

func sessionDeleteConfirmedCommand(sessionID string) string {
	return sessionCommand("delete-confirmed", sessionID)
}
