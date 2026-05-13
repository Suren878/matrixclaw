package controlplane

import "strings"

func PickerCommandFor(kind PickerKind, contextID string, itemID string) string {
	contextID = strings.TrimSpace(contextID)
	itemID = strings.TrimSpace(itemID)
	switch kind {
	case PickerSessions:
		if itemID == "" {
			return "/sessions"
		}
		if itemID == "cancel" {
			return ""
		}
		if itemID == "new" {
			return "/new"
		}
		return "/session menu " + itemID
	case PickerSessionRuntime:
		switch itemID {
		case "":
			return "/new"
		case "matrixclaw":
			return "/session new matrixclaw"
		case "codex":
			return "/session new codex"
		case "back":
			return "/sessions"
		case "cancel":
			return ""
		default:
			return "/new"
		}
	case PickerSessionActions:
		switch itemID {
		case "use":
			if contextID == "" {
				return "/sessions"
			}
			return "/session use " + contextID
		case "rename":
			if contextID == "" {
				return "/sessions"
			}
			return "/session rename " + contextID
		case "delete":
			if contextID == "" {
				return "/sessions"
			}
			return "/session delete " + contextID
		case "back", "cancel":
			return "/sessions"
		default:
			return "/sessions"
		}
	case PickerProvider:
		if itemID == "" {
			return "/provider"
		}
		if itemID == "cancel" {
			return ""
		}
		if itemID == "custom" {
			return "/provider custom"
		}
		return "/provider " + itemID
	case PickerProviderCustom:
		switch itemID {
		case "":
			return "/provider custom"
		case "openai":
			return "/provider custom openai"
		case "anthropic":
			return "/provider custom anthropic"
		case "back":
			return "/provider"
		case "cancel":
			return "/provider"
		default:
			return "/provider custom"
		}
	case PickerProviderActions:
		if contextID == "" {
			return "/provider"
		}
		switch itemID {
		case "":
			return "/provider " + contextID
		case "use":
			return "/provider use " + contextID
		case "edit":
			return "/provider edit " + contextID
		case "delete":
			return "/provider custom delete " + contextID
		case "back":
			return "/provider"
		case "cancel":
			return "/provider"
		default:
			return "/provider " + contextID
		}
	case PickerPermissions:
		if itemID == "" {
			return "/permissions"
		}
		if itemID == "cancel" {
			return ""
		}
		return "/permissions " + itemID
	case PickerModules:
		switch itemID {
		case "":
			return "/modules"
		case "storage":
			return "/modules storage"
		case "cancel":
			return ""
		default:
			return "/modules"
		}
	case PickerStorage:
		switch itemID {
		case "":
			return "/modules storage"
		case "import":
			return "/modules storage import"
		case "temp":
			return "/modules storage temp"
		case "files":
			return "/modules storage files"
		case "back":
			return "/modules"
		case "cancel":
			return ""
		default:
			return "/modules storage"
		}
	case PickerStorageFiles:
		switch {
		case itemID == "":
			return "/modules storage files"
		case itemID == "back":
			return "/modules storage"
		case itemID == "cancel":
			return ""
		case strings.HasPrefix(itemID, "file:"):
			return "/modules storage file " + strings.TrimPrefix(itemID, "file:")
		default:
			return "/modules storage files"
		}
	case PickerStorageFile:
		if contextID == "" {
			return "/modules storage files"
		}
		switch itemID {
		case "read":
			return "/modules storage read " + contextID
		case "delete":
			return "/modules storage delete " + contextID
		case "back":
			return "/modules storage files"
		case "cancel":
			return ""
		default:
			return "/modules storage file " + contextID
		}
	case PickerStorageTemp:
		switch {
		case itemID == "":
			return "/modules storage temp"
		case itemID == "cleanup":
			return "/modules storage temp-cleanup"
		case itemID == "toggle":
			return "/modules storage temp-cleanup-mode"
		case itemID == "days":
			return "/modules storage temp-days"
		case itemID == "max":
			return "/modules storage temp-max"
		case itemID == "back":
			return "/modules storage"
		case itemID == "cancel":
			return ""
		case strings.HasPrefix(itemID, "temp:"):
			return "/modules storage temp-file " + strings.TrimPrefix(itemID, "temp:")
		default:
			return "/modules storage temp"
		}
	case PickerStorageCleanup:
		switch itemID {
		case "on":
			return "/modules storage temp-toggle on"
		case "off":
			return "/modules storage temp-toggle off"
		case "back", "cancel":
			return "/modules storage temp"
		default:
			return "/modules storage temp-cleanup-mode"
		}
	case PickerStorageTempFile:
		if contextID == "" {
			return "/modules storage temp"
		}
		switch itemID {
		case "promote":
			return "/modules storage temp-promote " + contextID
		case "delete":
			return "/modules storage temp-delete " + contextID
		case "back":
			return "/modules storage temp"
		case "cancel":
			return ""
		default:
			return "/modules storage temp-file " + contextID
		}
	case PickerServer:
		switch itemID {
		case "":
			return "/server"
		case "status":
			return "/status"
		case "restart":
			return "/restart"
		case "cancel":
			return ""
		default:
			return "/server"
		}
	case PickerTasks:
		if itemID == "" {
			return "/tasks"
		}
		if itemID == "cancel" {
			return ""
		}
		if itemID == "archive" {
			return "/tasks archive"
		}
		if strings.HasPrefix(itemID, "open:") {
			return "/tasks menu " + strings.TrimPrefix(itemID, "open:")
		}
		if strings.HasPrefix(itemID, "closed:") {
			return "/tasks menu " + strings.TrimPrefix(itemID, "closed:")
		}
		return "/tasks"
	case PickerTaskActions:
		if contextID == "" {
			return "/tasks"
		}
		switch itemID {
		case "run":
			return "/tasks run " + contextID
		case "archive":
			return "/tasks complete " + contextID
		case "delete":
			return "/tasks delete " + contextID
		case "back", "cancel":
			return "/tasks"
		default:
			return "/tasks"
		}
	case PickerTaskArchive:
		switch itemID {
		case "":
			return "/tasks archive"
		case "delete_closed":
			return "/tasks delete-closed"
		case "back", "cancel":
			return "/tasks"
		default:
			if strings.HasPrefix(itemID, "closed:") {
				return "/tasks menu " + strings.TrimPrefix(itemID, "closed:")
			}
			return "/tasks archive"
		}
	default:
		return ""
	}
}
