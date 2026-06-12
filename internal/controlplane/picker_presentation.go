package controlplane

import "strings"

type pickerPresentationItem struct {
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

func pickerPresentationTitle(picker PickerData) string {
	if title := strings.TrimSpace(picker.Title); title != "" {
		return title
	}
	return PickerCommandLabel(picker)
}

func pickerLegend(picker PickerData) string {
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

func presentPickerItem(picker PickerData, item PickerItem) pickerPresentationItem {
	title := PickerItemDisplayTitle(item)
	info := PickerItemDisplayInfo(picker.Kind, item)
	presented := pickerPresentationItem{
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

func pickerPresentationSearch(presented pickerPresentationItem) string {
	if value := strings.TrimSpace(presented.Item.Search); value != "" {
		return value
	}
	return strings.TrimSpace(presented.Title + " " + presented.Status)
}

func pickerCompactLabel(kind PickerKind, presented pickerPresentationItem) string {
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
	case PickerRealtimeVoice:
		return "LIVE "
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
	case PickerBrowser:
		return "🌐 "
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
