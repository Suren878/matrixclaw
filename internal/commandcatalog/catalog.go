package commandcatalog

import "strings"

type CommandID string

const (
	CommandNewSession  CommandID = "new_session"
	CommandSessions    CommandID = "sessions"
	CommandSession     CommandID = "session"
	CommandProvider    CommandID = "provider"
	CommandPermissions CommandID = "permissions"
	CommandContext     CommandID = "context"
	CommandUsage       CommandID = "usage"
	CommandPlan        CommandID = "plan"
	CommandMemory      CommandID = "memory"
	CommandSearch      CommandID = "search"
	CommandSkills      CommandID = "skills"
	CommandModules     CommandID = "modules"
	CommandRemind      CommandID = "remind"
	CommandTasks       CommandID = "tasks"
	CommandServer      CommandID = "server"
	CommandStatus      CommandID = "status"
	CommandRestart     CommandID = "restart"
	CommandStop        CommandID = "stop"
	CommandHelp        CommandID = "help"
)

type CommandSpec struct {
	ID          CommandID
	Command     string
	Aliases     []string
	Description string
	Menu        bool
	Public      bool
}

func Catalog() []CommandSpec {
	return []CommandSpec{
		{ID: CommandNewSession, Command: "/new", Description: "New session", Menu: true, Public: false},
		{ID: CommandSessions, Command: "/sessions", Description: "Sessions", Menu: true, Public: true},
		{ID: CommandSession, Command: "/session", Description: "Session commands", Menu: false, Public: false},
		{ID: CommandProvider, Command: "/provider", Description: "Provider", Menu: true, Public: true},
		{ID: CommandPermissions, Command: "/permissions", Aliases: []string{"mode"}, Description: "Permission mode", Menu: true, Public: true},
		{ID: CommandContext, Command: "/context", Description: "Context", Menu: true, Public: true},
		{ID: CommandUsage, Command: "/usage", Description: "Token usage", Menu: false, Public: true},
		{ID: CommandPlan, Command: "/plan", Description: "Planning Mode", Menu: true, Public: true},
		{ID: CommandMemory, Command: "/memory", Description: "Memory", Menu: true, Public: true},
		{ID: CommandSearch, Command: "/search", Description: "Search history", Menu: false, Public: true},
		{ID: CommandSkills, Command: "/skills", Description: "Session skills", Menu: true, Public: true},
		{ID: CommandModules, Command: "/modules", Description: "Modules", Menu: true, Public: true},
		{ID: CommandRemind, Command: "/remind", Description: "Create reminder", Menu: false, Public: true},
		{ID: CommandTasks, Command: "/tasks", Description: "Tasks", Menu: true, Public: true},
		{ID: CommandServer, Command: "/server", Description: "Server", Menu: true, Public: true},
		{ID: CommandStatus, Command: "/status", Description: "Server status", Menu: false, Public: true},
		{ID: CommandRestart, Command: "/restart", Description: "Restart daemon", Menu: false, Public: true},
		{ID: CommandStop, Command: "/stop", Description: "Stop daemon", Menu: false, Public: true},
		{ID: CommandHelp, Command: "/help", Aliases: []string{"commands", "start"}, Description: "Help", Menu: false, Public: true},
	}
}

func Lookup(id CommandID) (CommandSpec, bool) {
	for _, spec := range Catalog() {
		if spec.ID == id {
			return spec, true
		}
	}
	return CommandSpec{}, false
}

func CommandLine(id CommandID, args string) string {
	spec, ok := Lookup(id)
	if !ok {
		return ""
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return spec.Command
	}
	return spec.Command + " " + args
}
