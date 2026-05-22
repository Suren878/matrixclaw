package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/commandcatalog"
)

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
	PickerSessions          PickerKind = "sessions"
	PickerSessionRuntime    PickerKind = "session_runtime"
	PickerSessionActions    PickerKind = "session_actions"
	PickerProvider          PickerKind = "provider"
	PickerProviderCustom    PickerKind = "provider_custom"
	PickerProviderActions   PickerKind = "provider_actions"
	PickerPermissions       PickerKind = "permissions"
	PickerContext           PickerKind = "context"
	PickerPlan              PickerKind = "plan"
	PickerModules           PickerKind = "modules"
	PickerTextToSpeech      PickerKind = "text_to_speech"
	PickerTTSProvider       PickerKind = "tts_provider"
	PickerSpeechToText      PickerKind = "speech_to_text"
	PickerVoiceEnabled      PickerKind = "voice_enabled"
	PickerVoiceProvider     PickerKind = "voice_provider"
	PickerExternalAgents    PickerKind = "external_agents"
	PickerExternalAgent     PickerKind = "external_agent"
	PickerExternalAgentOn   PickerKind = "external_agent_enabled"
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
	PickerSkillEnabled      PickerKind = "skill_enabled"
	PickerSkill             PickerKind = "skill"
	PickerMCP               PickerKind = "mcp"
	PickerMCPServer         PickerKind = "mcp_server"
	PickerMCPEnabled        PickerKind = "mcp_enabled"
	PickerMCPServerOn       PickerKind = "mcp_server_enabled"
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
	HideBackItem bool
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
	PickerItemRoleBack   PickerItemRole = "back"
	PickerItemRoleCancel PickerItemRole = "cancel"
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

func BackItem(label ...string) PickerItem {
	return PickerItem{ID: "back", Title: pickerItemLabel("Back", label...), Role: PickerItemRoleBack}
}

func CloseItem(label ...string) PickerItem {
	return PickerItem{ID: "cancel", Title: pickerItemLabel("Back", label...), Role: PickerItemRoleCancel, Command: ""}
}

func pickerItemLabel(fallback string, labels ...string) string {
	for _, label := range labels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func (item PickerItem) IsCancel() bool {
	return item.Role == PickerItemRoleCancel
}

func (item PickerItem) IsBack() bool {
	return item.Role == PickerItemRoleBack
}

func (item PickerItem) IsNavigation() bool {
	return item.IsCancel() || item.IsBack()
}

func (item PickerItem) IsDanger() bool {
	return item.Role == PickerItemRoleDanger
}

func (item PickerItem) IsAction() bool {
	return item.Role == PickerItemRoleAction
}

func (item PickerItem) NeedsSeparator() bool {
	switch item.Role {
	case PickerItemRoleCancel, PickerItemRoleBack, PickerItemRoleDanger, PickerItemRoleAction:
		return true
	default:
		return false
	}
}

func Catalog() []CommandSpec {
	return commandcatalog.Catalog()
}
