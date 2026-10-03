package telegram

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// approvalKeyboard answers one approval; "Always" keeps the suggested rule, and
// only the owner's chat may keep it for every session.
func approvalKeyboard(approval core.Approval, session bool, global bool) *InlineKeyboardMarkup {
	approvalID := approval.ID
	rows := [][]InlineKeyboardButton{{{Text: "✅ Allow", CallbackData: cbApprovalOnce + approvalID}}}
	if approval.Suggestion != nil && session {
		always := []InlineKeyboardButton{{Text: "♾ Always: session", CallbackData: cbApprovalSession + approvalID}}
		if global {
			always = append(always, InlineKeyboardButton{Text: "🌐 Always: global", CallbackData: cbApprovalGlobal + approvalID})
		}
		rows = append(rows, always)
	}
	rows = append(rows, []InlineKeyboardButton{
		{Text: "❌ Deny", CallbackData: cbApprovalDeny + approvalID},
		{Text: "✍️ Deny with reason", CallbackData: cbApprovalReason + approvalID},
	})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func formKeyboard(form controlplane.FormData) *InlineKeyboardMarkup {
	rows := make([][]InlineKeyboardButton, 0, len(form.Fields)+1)
	for _, field := range form.Fields {
		if strings.TrimSpace(field.EditCommand) == "" {
			continue
		}
		rows = append(rows, []InlineKeyboardButton{
			clippedCommandButton("✏️ "+textutil.FirstNonEmpty(field.Label, field.ID), field.EditCommand),
		})
	}
	rows = append(rows, []InlineKeyboardButton{
		commandButton("✅ "+textutil.FirstNonEmpty(form.SubmitLabel, "Save"), form.SubmitCommand),
		commandButton("✖️ "+textutil.FirstNonEmpty(form.CancelLabel, "Close"), form.CancelCommand),
	})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func confirmKeyboard(confirm controlplane.ConfirmData) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				commandButton("✅ "+textutil.FirstNonEmpty(confirm.ConfirmLabel, "Confirm"), confirm.ConfirmCommand),
				commandButton("✖️ "+textutil.FirstNonEmpty(confirm.CancelLabel, "Close"), confirm.CancelCommand),
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
