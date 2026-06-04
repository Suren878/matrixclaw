package controlplane

func tasksCommand(parts ...string) string {
	values := append([]string{"tasks"}, parts...)
	return controlplaneCommand(values...)
}

func tasksArchiveCommand() string {
	return tasksCommand("archive")
}

func taskMenuCommand(jobID string) string {
	return tasksCommand("menu", jobID)
}

func taskCompleteCommand(jobID string) string {
	return tasksCommand("complete", jobID)
}

func taskDeleteCommand(jobID string) string {
	return tasksCommand("delete", jobID)
}

func taskDeleteConfirmCommand(jobID string) string {
	return tasksCommand("delete-confirm", jobID)
}

func tasksDeleteClosedCommand() string {
	return tasksCommand("delete-closed")
}

func tasksDeleteClosedConfirmCommand() string {
	return tasksCommand("delete-closed-confirm")
}

func taskRunCommand(jobID string) string {
	return tasksCommand("run", jobID)
}
