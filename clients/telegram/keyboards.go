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

func pickerKeyboardView(picker controlplane.PickerData, view controlplane.ResultView) *InlineKeyboardMarkup {
	rows := make([][]InlineKeyboardButton, 0, len(view.Items)+1)
	for _, item := range view.Items {
		rows = append(rows, []InlineKeyboardButton{pickerButton(item)})
	}
	if view.Paging.Pages > 1 {
		var nav []InlineKeyboardButton
		if view.Paging.Page > 0 {
			nav = append(nav, InlineKeyboardButton{Text: "‹ Prev", CallbackData: pickerPageCallbackData(picker.Kind, picker.ContextID, view.Paging.Page-1)})
		}
		nav = append(nav, InlineKeyboardButton{Text: fmt.Sprintf("%d/%d", view.Paging.Page+1, view.Paging.Pages), CallbackData: pickerPageCallbackData(picker.Kind, picker.ContextID, view.Paging.Page)})
		if view.Paging.Page < view.Paging.Pages-1 {
			nav = append(nav, InlineKeyboardButton{Text: "Next ›", CallbackData: pickerPageCallbackData(picker.Kind, picker.ContextID, view.Paging.Page+1)})
		}
		rows = append(rows, nav)
	}
	if view.Footer != nil && !view.Footer.Hidden {
		rows = append(rows, []InlineKeyboardButton{footerButton(*view.Footer)})
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func pickerButton(item controlplane.ResultViewItem) InlineKeyboardButton {
	return clippedCommandButton(item.Label, item.Command)
}

func footerButton(footer controlplane.ResultViewFooter) InlineKeyboardButton {
	label := strings.TrimSpace(footer.Label)
	if label == "" {
		label = "Cancel"
	}
	return commandButton("‹ "+label, footer.Command)
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

func infoKeyboard(footer *controlplane.ResultViewFooter) *InlineKeyboardMarkup {
	if footer == nil || footer.Hidden {
		return nil
	}
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				footerButton(*footer),
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
	return InlineKeyboardButton{
		Text:         clipTelegramButtonText(telegramPersonaText(text)),
		CallbackData: commandCallbackData(command),
	}
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
