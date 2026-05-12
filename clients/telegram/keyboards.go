package telegram

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

func approvalKeyboard(approval core.Approval) *InlineKeyboardMarkup {
	approvalID := approval.ID
	firstRow := []InlineKeyboardButton{{Text: "✅ Allow", CallbackData: cbApprovalOnce + approvalID}}
	if canAllowSessionApproval(approval) {
		firstRow = append(firstRow, InlineKeyboardButton{Text: "🟢 Session", CallbackData: cbApprovalSession + approvalID})
	}
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			firstRow,
			{
				{Text: "❌ Deny", CallbackData: cbApprovalDeny + approvalID},
			},
		},
	}
}

func pickerKeyboard(picker controlplane.PickerData, page int) *InlineKeyboardMarkup {
	paged := controlplane.PaginatePicker(picker, page, modelPickerPageSize)
	rows := make([][]InlineKeyboardButton, 0, len(paged.Items)+len(paged.Trailing)+1)
	for _, item := range paged.Items {
		rows = append(rows, []InlineKeyboardButton{pickerButton(picker, item)})
	}
	if paged.Pages > 1 {
		var nav []InlineKeyboardButton
		if paged.Page > 0 {
			nav = append(nav, InlineKeyboardButton{Text: "‹ Prev", CallbackData: pickerPageCallbackData(picker.Kind, picker.ContextID, paged.Page-1)})
		}
		nav = append(nav, InlineKeyboardButton{Text: fmt.Sprintf("%d/%d", paged.Page+1, paged.Pages), CallbackData: pickerPageCallbackData(picker.Kind, picker.ContextID, paged.Page)})
		if paged.Page < paged.Pages-1 {
			nav = append(nav, InlineKeyboardButton{Text: "Next ›", CallbackData: pickerPageCallbackData(picker.Kind, picker.ContextID, paged.Page+1)})
		}
		rows = append(rows, nav)
	}
	for _, item := range paged.Trailing {
		rows = append(rows, []InlineKeyboardButton{pickerButton(picker, item)})
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func pickerButton(picker controlplane.PickerData, item controlplane.PickerItem) InlineKeyboardButton {
	presented := controlplane.PresentPickerItem(picker, item)
	return clippedCommandButton(presented.CompactLabel, presented.Command)
}

func formKeyboard(form controlplane.FormData) *InlineKeyboardMarkup {
	rows := make([][]InlineKeyboardButton, 0, len(form.Fields)+1)
	for _, field := range form.Fields {
		if strings.TrimSpace(field.EditCommand) == "" {
			continue
		}
		rows = append(rows, []InlineKeyboardButton{
			clippedCommandButton("✏️ "+firstNonEmpty(field.Label, field.ID), field.EditCommand),
		})
	}
	rows = append(rows, []InlineKeyboardButton{
		commandButton("✅ "+firstNonEmpty(form.SubmitLabel, "Save"), form.SubmitCommand),
		commandButton("✖️ "+firstNonEmpty(form.CancelLabel, "Cancel"), form.CancelCommand),
	})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func confirmKeyboard(confirm controlplane.ConfirmData) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				commandButton("✅ "+firstNonEmpty(confirm.ConfirmLabel, "Confirm"), confirm.ConfirmCommand),
				commandButton("✖️ "+firstNonEmpty(confirm.CancelLabel, "Cancel"), confirm.CancelCommand),
			},
		},
	}
}

func infoKeyboard(info controlplane.InfoData) *InlineKeyboardMarkup {
	if strings.TrimSpace(info.CloseCommand) == "" {
		return nil
	}
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				commandButton("‹ Back", info.CloseCommand),
			},
		},
	}
}

func commandButton(text string, command string) InlineKeyboardButton {
	return InlineKeyboardButton{
		Text:         telegramPersonaText(text),
		CallbackData: commandCallbackData(command),
	}
}

func clippedCommandButton(text string, command string) InlineKeyboardButton {
	return commandButton(clipTelegramButtonText(telegramPersonaText(text)), command)
}

func clipTelegramButtonText(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= defaultButtonTextLimit {
		return text
	}
	return strings.TrimSpace(string(runes[:defaultButtonTextLimit-1])) + "…"
}
