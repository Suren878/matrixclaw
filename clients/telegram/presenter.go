package telegram

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

type commandPresentation struct {
	Text        string
	ReplyMarkup *InlineKeyboardMarkup
}

// presentCommandResult renders a command result as a message; page -1 opens
// a picker on the page of its selected row.
func presentCommandResult(result controlplane.Result, page int) commandPresentation {
	var text string
	var markup *InlineKeyboardMarkup
	switch {
	case result.Picker != nil:
		text, markup = presentPicker(*result.Picker, page)
	case result.Form != nil:
		text, markup = formText(*result.Form), formKeyboard(*result.Form)
	case result.Prompt != nil:
		text = textutil.FirstNonEmpty(result.Prompt.Title, "Send the new value.") + "\n/close to close"
	case result.TextEdit != nil:
		text = strings.TrimSpace(result.TextEdit.Value)
	case result.Confirm != nil:
		text, markup = confirmText(*result.Confirm), confirmKeyboard(*result.Confirm)
	case result.Info != nil:
		text = infoText(*result.Info)
		markup = &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{commandButton("‹ Close", result.Info.CloseCommand)}}}
	default:
		text = strings.TrimSpace(result.Text)
	}
	return commandPresentation{Text: telegramPersonaText(text), ReplyMarkup: markup}
}

// presentPicker shows one page of the picker's rows as buttons; a picker that
// cannot be shown again (no Command) is shown whole.
func presentPicker(picker controlplane.PickerData, page int) (string, *InlineKeyboardMarkup) {
	text := strings.TrimSpace(picker.Title)
	if text == "" {
		text = "Choose:"
	} else if meta := strings.TrimSpace(picker.Meta); meta != "" {
		text += "\n" + meta
	}
	items, pages := picker.Items, 1
	if picker.Command == "" || len(items) <= modelPickerPageSize {
		page = 0
	} else {
		pages = (len(items) + modelPickerPageSize - 1) / modelPickerPageSize
		if page < 0 {
			page = max(0, slicesIndexSelected(items)) / modelPickerPageSize
		}
		page = min(max(page, 0), pages-1)
		items = items[page*modelPickerPageSize : min((page+1)*modelPickerPageSize, len(items))]
	}
	rows := make([][]InlineKeyboardButton, 0, len(items)+2)
	for _, item := range items {
		button := clippedCommandButton(pickerItemLabel(picker.Kind, item), item.Command)
		if strings.TrimSpace(item.Command) == "" {
			button.CallbackData = cbNoop
		}
		rows = append(rows, []InlineKeyboardButton{button})
	}
	if pages > 1 {
		var nav []InlineKeyboardButton
		if page > 0 {
			nav = append(nav, InlineKeyboardButton{Text: "‹ Prev", CallbackData: pickerPageCallbackData(picker.Command, page-1)})
		}
		nav = append(nav, InlineKeyboardButton{Text: fmt.Sprintf("%d/%d", page+1, pages), CallbackData: pickerPageCallbackData(picker.Command, page)})
		if page < pages-1 {
			nav = append(nav, InlineKeyboardButton{Text: "Next ›", CallbackData: pickerPageCallbackData(picker.Command, page+1)})
		}
		rows = append(rows, nav)
	}
	switch {
	case picker.Back != "":
		rows = append(rows, []InlineKeyboardButton{commandButton("‹ Back", picker.Back)})
	default:
		rows = append(rows, []InlineKeyboardButton{commandButton("‹ Close", picker.Close)})
	}
	return text, &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func slicesIndexSelected(items []controlplane.PickerItem) int {
	for index, item := range items {
		if item.Selected {
			return index
		}
	}
	return 0
}

// pickerItemLabel is a row's button text: a marker for its kind or role, the
// title and its info, and ✅ when it is selected.
func pickerItemLabel(kind controlplane.PickerKind, item controlplane.PickerItem) string {
	label := textutil.FirstNonEmpty(item.Title, item.ID)
	if info := strings.TrimSpace(item.Info); info != "" {
		label += " · " + info
	}
	label = pickerItemPrefix(kind, item) + label
	if item.Selected {
		label = "✅ " + label
	}
	return label
}

func pickerItemPrefix(kind controlplane.PickerKind, item controlplane.PickerItem) string {
	switch item.Role {
	case controlplane.PickerItemRoleDanger:
		return "🗑️ "
	case controlplane.PickerItemRoleAction:
		if kind == controlplane.PickerSessions || kind == controlplane.PickerProvider {
			return "➕ "
		}
		return "▶️ "
	}
	switch kind {
	case controlplane.PickerSessions:
		return "💬 "
	case controlplane.PickerSessionActions, controlplane.PickerProviderActions:
		return map[string]string{"use": "✅ ", "rename": "✏️ ", "edit": "✏️ ", "delete": "🗑️ "}[item.ID]
	case controlplane.PickerProvider, controlplane.PickerProviderCustom, controlplane.PickerMCP, controlplane.PickerMCPServer:
		return "🔌 "
	case controlplane.PickerSkills, controlplane.PickerSkillsSection, controlplane.PickerSkill, controlplane.PickerSessionSkills, controlplane.PickerSessionSkill:
		return "📘 "
	case controlplane.PickerServer:
		return map[string]string{"status": "📊 ", "restart": "🔄 "}[item.ID]
	}
	return ""
}

func formText(form controlplane.FormData) string {
	lines := []string{textutil.FirstNonEmpty(form.Title, "Form")}
	for _, field := range form.Fields {
		if label := strings.TrimSpace(field.Label); label != "" {
			lines = append(lines, label+": "+textutil.FirstNonEmpty(field.Value, "Empty"))
		}
	}
	return strings.Join(lines, "\n")
}

func confirmText(confirm controlplane.ConfirmData) string {
	text := strings.TrimSpace(confirm.Message)
	if title := strings.TrimSpace(confirm.Title); title != "" {
		if text == "" {
			return title
		}
		return title + "\n\n" + text
	}
	return text
}

// infoText is the info's text, or its rows when it has none; a text repeats
// the rows.
func infoText(info controlplane.InfoData) string {
	var lines []string
	if title := strings.TrimSpace(info.Title); title != "" {
		lines = append(lines, title)
	}
	if text := strings.TrimSpace(info.Text); text != "" {
		return strings.Join(append(lines, text), "\n")
	}
	for _, row := range info.Rows {
		label, value := strings.TrimSpace(row.Label), strings.TrimSpace(row.Value)
		switch {
		case label != "" && value != "":
			lines = append(lines, label+": "+value)
		case label != "" || value != "":
			lines = append(lines, label+value)
		}
	}
	return strings.Join(lines, "\n")
}
