package telegram

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

type commandPresentation struct {
	Text        string
	ReplyMarkup *InlineKeyboardMarkup
}

func presentCommandResult(result controlplane.Result, page int) commandPresentation {
	switch {
	case result.Picker != nil:
		return commandPresentation{
			Text:        telegramPersonaText(controlplane.PickerPresentationText(*result.Picker)),
			ReplyMarkup: pickerKeyboard(*result.Picker, page),
		}
	case result.Form != nil:
		return commandPresentation{
			Text:        telegramPersonaText(controlplane.FormPresentationText(*result.Form)),
			ReplyMarkup: formKeyboard(*result.Form),
		}
	case result.Prompt != nil:
		return commandPresentation{
			Text: telegramPersonaText(promptText(*result.Prompt)),
		}
	case result.Confirm != nil:
		return commandPresentation{
			Text:        telegramPersonaText(controlplane.ConfirmPresentationText(*result.Confirm)),
			ReplyMarkup: confirmKeyboard(*result.Confirm),
		}
	case result.Info != nil:
		return commandPresentation{
			Text:        telegramPersonaText(controlplane.InfoPresentationText(*result.Info)),
			ReplyMarkup: infoKeyboard(*result.Info),
		}
	default:
		return commandPresentation{
			Text: telegramPersonaText(result.Text),
		}
	}
}

func promptText(prompt controlplane.PromptData) string {
	text := strings.TrimSpace(prompt.Title)
	if text == "" {
		text = "Send the new value."
	}
	return text + "\n/cancel to abort"
}
