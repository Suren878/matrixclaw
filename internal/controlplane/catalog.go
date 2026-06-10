package controlplane

import "github.com/Suren878/matrixclaw/internal/commandcatalog"

type CommandID = commandcatalog.CommandID

const (
	CommandNewSession  = commandcatalog.CommandNewSession
	CommandSessions    = commandcatalog.CommandSessions
	CommandSession     = commandcatalog.CommandSession
	CommandProvider    = commandcatalog.CommandProvider
	CommandPermissions = commandcatalog.CommandPermissions
	CommandContext     = commandcatalog.CommandContext
	CommandUsage       = commandcatalog.CommandUsage
	CommandPlan        = commandcatalog.CommandPlan
	CommandMemory      = commandcatalog.CommandMemory
	CommandSearch      = commandcatalog.CommandSearch
	CommandSkills      = commandcatalog.CommandSkills
	CommandModules     = commandcatalog.CommandModules
	CommandRemind      = commandcatalog.CommandRemind
	CommandTasks       = commandcatalog.CommandTasks
	CommandServer      = commandcatalog.CommandServer
	CommandStatus      = commandcatalog.CommandStatus
	CommandRestart     = commandcatalog.CommandRestart
	CommandStop        = commandcatalog.CommandStop
	CommandHelp        = commandcatalog.CommandHelp
)

type PickerKind string

const (
	PickerCommandMenu       PickerKind = "command_menu"
	PickerSessions          PickerKind = "sessions"
	PickerSessionRuntime    PickerKind = "session_runtime"
	PickerSessionActions    PickerKind = "session_actions"
	PickerSessionModels     PickerKind = "session_models"
	PickerProvider          PickerKind = "provider"
	PickerProviderCustom    PickerKind = "provider_custom"
	PickerProviderActions   PickerKind = "provider_actions"
	PickerPermissions       PickerKind = "permissions"
	PickerContext           PickerKind = "context"
	PickerPlan              PickerKind = "plan"
	PickerModules           PickerKind = "modules"
	PickerTextToSpeech      PickerKind = "text_to_speech"
	PickerSpeechToText      PickerKind = "speech_to_text"
	PickerVoiceProvider     PickerKind = "voice_provider"
	PickerExternalAgents    PickerKind = "external_agents"
	PickerExternalAgent     PickerKind = "external_agent"
	PickerStorage           PickerKind = "storage"
	PickerStorageFiles      PickerKind = "storage_files"
	PickerStorageFile       PickerKind = "storage_file"
	PickerStorageTemp       PickerKind = "storage_temp"
	PickerStorageCleanup    PickerKind = "storage_cleanup"
	PickerStorageTempFile   PickerKind = "storage_temp_file"
	PickerSessionSkills     PickerKind = "session_skills"
	PickerSessionSkill      PickerKind = "session_skill"
	PickerSkills            PickerKind = "skills"
	PickerSkillsSection     PickerKind = "skills_section"
	PickerSkill             PickerKind = "skill"
	PickerMCP               PickerKind = "mcp"
	PickerBrowser           PickerKind = "browser"
	PickerMCPServer         PickerKind = "mcp_server"
	PickerTasks             PickerKind = "tasks"
	PickerTaskActions       PickerKind = "task_actions"
	PickerTaskArchive       PickerKind = "task_archive"
	PickerServer            PickerKind = "server"
	PickerWebSearch         PickerKind = "web_search"
	PickerWebSearchProvider PickerKind = "web_search_provider"
)

type CommandSpec = commandcatalog.CommandSpec

type PickerData struct {
	Kind         PickerKind
	ContextID    string
	Title        string
	Meta         string
	BackCommand  string
	CloseCommand string
	HasBack      bool
	HasClose     bool
	Popup        bool
	Items        []PickerItem
}

type MenuItemGroup string

const (
	MenuItemGroupPrimary   MenuItemGroup = ""
	MenuItemGroupSecondary MenuItemGroup = "secondary"
)

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

func (item PickerItem) IsDanger() bool {
	return item.Role == PickerItemRoleDanger
}

func (item PickerItem) IsAction() bool {
	return item.Role == PickerItemRoleAction
}

func (item PickerItem) NeedsSeparator() bool {
	switch item.Role {
	case PickerItemRoleDanger, PickerItemRoleAction:
		return true
	default:
		return false
	}
}

func Catalog() []CommandSpec {
	return commandcatalog.Catalog()
}
