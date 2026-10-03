package controlplane

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
	CommandContinue    CommandID = "continue"
	CommandBudget      CommandID = "budget"
	CommandTodo        CommandID = "todo"
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
	CommandApproval    CommandID = "approval"
)

type CommandSpec struct {
	ID      CommandID
	Command string
	Aliases []string
	Title   string
	Menu    bool
	Public  bool
}

var commandCatalog = []CommandSpec{
	{ID: CommandNewSession, Command: "/new", Title: "New Session", Menu: true},
	{ID: CommandSessions, Command: "/sessions", Title: "Sessions", Menu: true, Public: true},
	{ID: CommandSession, Command: "/session", Title: "Session commands"},
	{ID: CommandProvider, Command: "/provider", Title: "Provider", Menu: true, Public: true},
	{ID: CommandPermissions, Command: "/permissions", Aliases: []string{"mode"}, Title: "Permission Mode", Menu: true, Public: true},
	{ID: CommandContext, Command: "/context", Title: "Context", Menu: true, Public: true},
	{ID: CommandUsage, Command: "/usage", Title: "Token usage", Public: true},
	{ID: CommandContinue, Command: "/continue", Title: "Continue the last run", Public: true},
	{ID: CommandBudget, Command: "/budget", Title: "Run budget", Public: true},
	{ID: CommandTodo, Command: "/todo", Title: "Todo", Menu: true, Public: true},
	{ID: CommandMemory, Command: "/memory", Title: "Memory", Menu: true, Public: true},
	{ID: CommandSearch, Command: "/search", Title: "Search history", Public: true},
	{ID: CommandSkills, Command: "/skills", Title: "Session skills", Menu: true, Public: true},
	{ID: CommandModules, Command: "/modules", Title: "Modules", Menu: true, Public: true},
	{ID: CommandRemind, Command: "/remind", Title: "Reminder", Public: true},
	{ID: CommandTasks, Command: "/tasks", Title: "Tasks", Menu: true, Public: true},
	{ID: CommandServer, Command: "/server", Title: "Server", Menu: true, Public: true},
	{ID: CommandStatus, Command: "/status", Title: "Server Status", Public: true},
	{ID: CommandRestart, Command: "/restart", Title: "Restart Daemon", Public: true},
	{ID: CommandStop, Command: "/stop", Title: "Stop Daemon", Public: true},
	{ID: CommandHelp, Command: "/help", Aliases: []string{"commands", "start"}, Title: "Help", Public: true},
	{ID: CommandApproval, Command: "/approval", Title: "Answer an approval"},
}

func Catalog() []CommandSpec {
	return commandCatalog
}

// CommandLine is the command text that runs id with args.
func CommandLine(id CommandID, args string) string {
	for _, spec := range commandCatalog {
		if spec.ID != id {
			continue
		}
		if args = strings.TrimSpace(args); args != "" {
			return spec.Command + " " + args
		}
		return spec.Command
	}
	return ""
}

type PickerKind string

const (
	PickerCommandMenu     PickerKind = "command_menu"
	PickerSessions        PickerKind = "sessions"
	PickerSessionRuntime  PickerKind = "session_runtime"
	PickerSessionActions  PickerKind = "session_actions"
	PickerSessionModels   PickerKind = "session_models"
	PickerProvider        PickerKind = "provider"
	PickerProviderCustom  PickerKind = "provider_custom"
	PickerProviderActions PickerKind = "provider_actions"
	PickerPermissions     PickerKind = "permissions"
	PickerContext         PickerKind = "context"
	PickerModules         PickerKind = "modules"
	PickerModule          PickerKind = "module"
	PickerExternalAgents  PickerKind = "external_agents"
	PickerExternalAgent   PickerKind = "external_agent"
	PickerStorage         PickerKind = "storage"
	PickerStorageFiles    PickerKind = "storage_files"
	PickerStorageFile     PickerKind = "storage_file"
	PickerStorageTemp     PickerKind = "storage_temp"
	PickerStorageCleanup  PickerKind = "storage_cleanup"
	PickerStorageTempFile PickerKind = "storage_temp_file"
	PickerSessionSkills   PickerKind = "session_skills"
	PickerSessionSkill    PickerKind = "session_skill"
	PickerSkills          PickerKind = "skills"
	PickerSkillsSection   PickerKind = "skills_section"
	PickerSkill           PickerKind = "skill"
	PickerMCP             PickerKind = "mcp"
	PickerMCPServer       PickerKind = "mcp_server"
	PickerTasks           PickerKind = "tasks"
	PickerTaskActions     PickerKind = "task_actions"
	PickerTaskArchive     PickerKind = "task_archive"
	PickerServer          PickerKind = "server"
)

// PickerData is a list screen; clients render it as they see fit (Telegram
// buttons, a terminal menu) from these fields alone.
type PickerData struct {
	Kind  PickerKind
	Title string
	Meta  string
	// Command shows the picker again (Telegram pages with it); empty when
	// the picker cannot be repeated, and then it is shown unpaged.
	Command string
	Back    string // command of a Back button; empty for none
	// Popup is a choice that closes once picked; Close runs on dismissal.
	Popup bool
	Close string
	Items []PickerItem
}

type PickerItemRole string

const (
	PickerItemRoleNormal PickerItemRole = ""
	PickerItemRoleDanger PickerItemRole = "danger"
	PickerItemRoleAction PickerItemRole = "action"
)

type PickerItem struct {
	ID       string
	Title    string
	Info     string
	Search   string
	Command  string
	Selected bool
	Focused  bool
	Disabled bool
	Role     PickerItemRole
}

// NeedsSeparator reports whether a list draws a line above item.
func (item PickerItem) NeedsSeparator() bool {
	switch item.Role {
	case PickerItemRoleDanger, PickerItemRoleAction:
		return true
	default:
		return false
	}
}
