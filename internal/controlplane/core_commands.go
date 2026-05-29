package controlplane

func permissionsCommand(parts ...string) string {
	values := append([]string{"permissions"}, parts...)
	return controlplaneCommand(values...)
}

func contextCommand(parts ...string) string {
	values := append([]string{"context"}, parts...)
	return controlplaneCommand(values...)
}

func contextInfoCommand() string {
	return contextCommand("info")
}

func contextClearCommand() string {
	return contextCommand("clear")
}

func contextClearConfirmCommand() string {
	return contextCommand("clear", "confirm")
}

func contextCompactCommand() string {
	return contextCommand("compact")
}

func contextCompactConfirmCommand() string {
	return contextCommand("compact", "confirm")
}

func serverCommand() string {
	return controlplaneCommand("server")
}

func statusCommand() string {
	return controlplaneCommand("status")
}

func restartCommand(parts ...string) string {
	values := append([]string{"restart"}, parts...)
	return controlplaneCommand(values...)
}

func restartConfirmCommand() string {
	return restartCommand("confirm")
}

func stopCommand(parts ...string) string {
	values := append([]string{"stop"}, parts...)
	return controlplaneCommand(values...)
}

func stopConfirmCommand() string {
	return stopCommand("confirm")
}
