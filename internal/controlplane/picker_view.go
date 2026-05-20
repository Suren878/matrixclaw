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
	if command := strings.TrimSpace(item.Command); command != "" {
		return command
	}
	if command, ok := pickerItemNavigationCommand(picker, item); ok {
		return command
	}
	return PickerCommandFor(picker.Kind, picker.ContextID, item.ID)
}

func pickerItemNavigationCommand(picker PickerData, item PickerItem) (string, bool) {
	switch {
	case item.IsBack():
		return pickerBackCommand(picker)
	case item.IsCancel():
		return pickerCloseCommand(picker)
	default:
		return "", false
	}
}

func PickerCloseCommand(picker PickerData) string {
	if command, ok := pickerBackCommand(picker); ok {
		return command
	}
	if command, ok := pickerCloseCommand(picker); ok {
		return command
	}
	return PickerCommandFor(picker.Kind, picker.ContextID, "cancel")
}

func pickerBackCommand(picker PickerData) (string, bool) {
	if !picker.HasBack {
		return "", false
	}
	return strings.TrimSpace(picker.BackCommand), true
}

func pickerCloseCommand(picker PickerData) (string, bool) {
	if !picker.HasClose {
		return "", false
	}
	return strings.TrimSpace(picker.CloseCommand), true
}

func PickerCommandLabel(picker PickerData) string {
	switch picker.Kind {
	case PickerSessions:
		return sessionsCommand()
	case PickerSessionActions:
		return sessionCommand()
	case PickerProvider, PickerProviderCustom, PickerProviderActions:
		return providerCommand()
	case PickerPermissions:
		return permissionsCommand()
	case PickerContext:
		return contextCommand()
	case PickerModules, PickerTextToSpeech, PickerSpeechToText, PickerVoiceEnabled, PickerVoiceProvider, PickerExternalAgents, PickerExternalAgent, PickerExternalAgentOn, PickerStorage, PickerStorageFiles, PickerStorageFile, PickerStorageTemp, PickerStorageCleanup, PickerStorageTempFile:
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
	trailing := []PickerItem{}
	items := make([]PickerItem, 0, len(picker.Items))
	for _, item := range picker.Items {
		if item.IsCancel() {
			trailing = append(trailing, item)
			continue
		}
		items = append(items, item)
	}
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
		Items:    append([]PickerItem(nil), items[start:end]...),
		Trailing: trailing,
		Page:     page,
		Pages:    pages,
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
	if item.IsNavigation() {
		return ""
	}
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
