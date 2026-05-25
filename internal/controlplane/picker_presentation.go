package controlplane

import "strings"

type PickerPresentationItem struct {
	Item            PickerItem
	Command         string
	Title           string
	Info            string
	Status          string
	Search          string
	CompactLabel    string
	Selected        bool
	Disabled        bool
	SeparatorBefore bool
}

func PresentPickerItems(picker PickerData) []PickerPresentationItem {
	viewItems := PickerViewItems(picker)
	out := make([]PickerPresentationItem, 0, len(viewItems))
	for _, item := range viewItems {
		presented := presentPickerItem(picker, item.Item)
		presented.Command = item.Command
		presented.SeparatorBefore = item.SeparatorBefore
		out = append(out, presented)
	}
	return out
}

func PresentPickerItem(picker PickerData, item PickerItem) PickerPresentationItem {
	return presentPickerItem(picker, item)
}

func PickerPresentationTitle(picker PickerData) string {
	if title := strings.TrimSpace(picker.Title); title != "" {
		return title
	}
	return PickerCommandLabel(picker)
}

func PickerLegend(picker PickerData) string {
	closeLabel := "back"
	switch picker.Kind {
	case PickerSessions:
		return "enter open · esc " + closeLabel
	case PickerSessionActions, PickerProviderActions, PickerContext, PickerTasks, PickerTaskActions, PickerTaskArchive, PickerServer:
		return "enter run · esc " + closeLabel
	case PickerProvider, PickerProviderCustom:
		return "enter select · esc " + closeLabel
	case PickerPermissions:
		return "enter apply · esc " + closeLabel
	default:
		return "enter select · esc " + closeLabel
	}
}

func presentPickerItem(picker PickerData, item PickerItem) PickerPresentationItem {
	title := PickerItemDisplayTitle(item)
	info := PickerItemDisplayInfo(picker.Kind, item)
	presented := PickerPresentationItem{
		Item:     item,
		Command:  PickerItemCommand(picker, item),
		Title:    title,
		Info:     info,
		Selected: item.Selected,
		Disabled: item.Disabled,
	}
	presented.Status = presented.Info
	presented.Search = pickerPresentationSearch(presented)
	presented.CompactLabel = pickerCompactLabel(picker.Kind, presented)
	return presented
}

func pickerPresentationSearch(presented PickerPresentationItem) string {
	if value := strings.TrimSpace(presented.Item.Search); value != "" {
		return value
	}
	return strings.TrimSpace(presented.Title + " " + presented.Status)
}

func pickerCompactLabel(kind PickerKind, presented PickerPresentationItem) string {
	title := presented.Title
	if presented.Info != "" {
		title += " · " + presented.Info
	}
	prefix := pickerCompactPrefix(kind, presented.Item)
	return prefix + title
}

func pickerCompactPrefix(kind PickerKind, item PickerItem) string {
	itemID := strings.TrimSpace(item.ID)
	switch {
	case item.IsDanger():
		return "🗑️ "
	case item.IsAction():
		switch kind {
		case PickerSessions, PickerProvider:
			return "➕ "
		default:
			return "▶️ "
		}
	}
	switch kind {
	case PickerSessions:
		return "💬 "
	case PickerSessionActions:
		switch itemID {
		case "use":
			return "✅ "
		case "rename":
			return "✏️ "
		case "delete":
			return "🗑️ "
		default:
			return ""
		}
	case PickerProvider, PickerProviderCustom:
		return "🔌 "
	case PickerTextToSpeech:
		return "TTS "
	case PickerSpeechToText:
		return "STT "
	case PickerProviderActions:
		switch itemID {
		case "use":
			return "✅ "
		case "edit":
			return "✏️ "
		case "delete":
			return "🗑️ "
		default:
			return ""
		}
	case PickerSkills, PickerSkillsSection, PickerSkill, PickerSessionSkills, PickerSessionSkill:
		return "📘 "
	case PickerMCP, PickerMCPServer:
		return "🔌 "
	case PickerServer:
		switch itemID {
		case "status":
			return "📊 "
		case "restart":
			return "🔄 "
		default:
			return ""
		}
	default:
		return ""
	}
}
