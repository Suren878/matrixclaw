package controlplane

import "strings"

type PickerViewItem struct {
	Item            PickerItem
	Command         string
	SeparatorBefore bool
}

type PickerPage struct {
	Items    []PickerItem
	Trailing []PickerItem
	Page     int
	Pages    int
}

type PickerFooterAction struct {
	Label   string
	Command string
}

func PickerViewItems(picker PickerData) []PickerViewItem {
	out := make([]PickerViewItem, 0, len(picker.Items))
	for _, item := range picker.Items {
		separator := item.NeedsSeparator() && len(out) > 0
		out = append(out, PickerViewItem{
			Item:            item,
			Command:         PickerItemCommand(picker, item),
			SeparatorBefore: separator,
		})
	}
	return out
}

func PickerItemCommand(picker PickerData, item PickerItem) string {
	return strings.TrimSpace(item.Command)
}

func PickerFooter(picker PickerData) (PickerFooterAction, bool) {
	if picker.HasBack {
		return PickerFooterAction{
			Label:   "Back",
			Command: strings.TrimSpace(picker.BackCommand),
		}, true
	}
	if picker.HasClose {
		return PickerFooterAction{
			Label:   "Close",
			Command: strings.TrimSpace(picker.CloseCommand),
		}, true
	}
	return PickerFooterAction{}, false
}

func PickerCommandLabel(picker PickerData) string {
	switch picker.Kind {
	case PickerCommandMenu:
		return helpCommand()
	case PickerSessions:
		return sessionsCommand()
	case PickerSessionActions, PickerSessionModels:
		return sessionCommand()
	case PickerProvider, PickerProviderCustom, PickerProviderActions:
		return providerCommand()
	case PickerPermissions:
		return permissionsCommand()
	case PickerContext:
		return contextCommand()
	case PickerSessionSkills, PickerSessionSkill:
		return sessionSkillsCommand()
	case PickerModules, PickerTextToSpeech, PickerSpeechToText, PickerRealtimeVoice, PickerTelephony, PickerVoiceProvider, PickerExternalAgents, PickerExternalAgent, PickerStorage, PickerStorageFiles, PickerStorageFile, PickerStorageTemp, PickerStorageCleanup, PickerStorageTempFile, PickerSkills, PickerSkillsSection, PickerSkill, PickerBrowser, PickerMCP, PickerMCPServer:
		return modulesCommand()
	case PickerTasks, PickerTaskActions, PickerTaskArchive:
		return tasksCommand()
	case PickerServer:
		return serverCommand()
	default:
		return strings.TrimSpace(picker.Title)
	}
}

func PickerSelectedPage(items []PickerItem, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	for index, item := range items {
		if item.Selected {
			return index / pageSize
		}
	}
	return 0
}

func PaginatePicker(picker PickerData, page int, pageSize int) PickerPage {
	if pageSize <= 0 {
		return PickerPage{Items: append([]PickerItem(nil), picker.Items...), Page: 0, Pages: 1}
	}
	items := append([]PickerItem(nil), picker.Items...)
	if len(items) <= pageSize {
		return PickerPage{Items: append([]PickerItem(nil), picker.Items...), Page: 0, Pages: 1}
	}
	pages := (len(items) + pageSize - 1) / pageSize
	if page < 0 {
		page = PickerSelectedPage(items, pageSize)
	}
	if page >= pages {
		page = pages - 1
	}
	if page < 0 {
		page = 0
	}
	start := page * pageSize
	end := min(start+pageSize, len(items))
	return PickerPage{
		Items: append([]PickerItem(nil), items[start:end]...),
		Page:  page,
		Pages: pages,
	}
}

func PickerItemDisplayTitle(item PickerItem) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = strings.TrimSpace(item.ID)
	}
	return strings.TrimSpace(strings.TrimPrefix(title, "✅ "))
}

func PickerItemDisplayInfo(kind PickerKind, item PickerItem) string {
	info := strings.TrimSpace(item.Info)
	if info == "" {
		return ""
	}
	if kind == PickerProvider {
		title := PickerItemDisplayTitle(item)
		if strings.EqualFold(info, strings.TrimSpace(item.ID)) || strings.EqualFold(info, title) {
			return ""
		}
	}
	return info
}
