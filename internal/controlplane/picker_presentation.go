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
	Navigation      bool
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
	case PickerPermissions, PickerExternalAgentOn, PickerVoiceEnabled:
		return "enter apply · esc " + closeLabel
	default:
		return "enter select · esc " + closeLabel
	}
}

func pickerPresentationStatus(kind PickerKind, selected bool, info string) string {
	if !selected {
		return info
	}
	info = strings.TrimSpace(info)
	switch kind {
	case PickerTextToSpeech, PickerSpeechToText:
		return info
	case PickerTTSProvider:
		return info
	case PickerVoiceProvider:
		return trimLeadingActiveStatus(info)
	case PickerExternalAgents, PickerExternalAgentOn:
		return info
	}
	info = trimLeadingActiveStatus(info)
	if info == "" {
		return "Active"
	}
	return "Active · " + info
}

func trimLeadingActiveStatus(info string) string {
	parts := strings.Split(info, "·")
	for len(parts) > 0 && strings.EqualFold(strings.TrimSpace(parts[0]), "Active") {
		parts = parts[1:]
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.TrimSpace(strings.Join(nonEmptyStrings(parts...), " · "))
}

func presentPickerItem(picker PickerData, item PickerItem) PickerPresentationItem {
	title := PickerItemDisplayTitle(item)
	info := PickerItemDisplayInfo(picker.Kind, item)
	presented := PickerPresentationItem{
		Item:       item,
		Command:    PickerItemCommand(picker, item),
		Title:      title,
		Info:       info,
		Selected:   item.Selected,
		Disabled:   item.Disabled,
		Navigation: item.IsNavigation(),
	}
	presented.Status = pickerPresentationStatus(picker.Kind, presented.Selected, presented.Info)
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
	if label, ok := pickerNavigationCompactLabel(presented); ok {
		return label
	}
	title := presented.Title
	if presented.Info != "" {
		title += " · " + presented.Info
	}
	prefix := pickerCompactPrefix(kind, presented.Item)
	if presented.Selected && kind != PickerTextToSpeech && kind != PickerSpeechToText {
		return "✅ " + prefix + title
	}
	return prefix + title
}

func pickerNavigationCompactLabel(presented PickerPresentationItem) (string, bool) {
	command := strings.TrimSpace(presented.Command)
	switch {
	case presented.Item.IsBack():
		if command == "" {
			return "✖️ Cancel", true
		}
		return "‹ Back", true
	case presented.Item.IsCancel():
		if command == "" {
			return "✖️ Cancel", true
		}
		return "‹ Back", true
	default:
		return "", false
	}
}

func pickerCompactPrefix(kind PickerKind, item PickerItem) string {
	itemID := strings.TrimSpace(item.ID)
	switch {
	case item.IsCancel():
		return "✖️ "
	case item.IsBack():
		return "‹ "
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
	case PickerTextToSpeech, PickerTTSProvider, PickerVoiceProvider:
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
	case PickerExternalAgentOn:
		return "⚙️ "
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
