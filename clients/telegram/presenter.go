package telegram

import "github.com/Suren878/matrixclaw/internal/controlplane"

type commandPresentation struct {
	Text        string
	ReplyMarkup *InlineKeyboardMarkup
}

func presentCommandResult(result controlplane.Result, page int) commandPresentation {
	view := controlplane.PresentResult(result, controlplane.ResultViewOptions{
		Surface:  controlplane.SurfaceTelegram,
		Page:     page,
		PageSize: modelPickerPageSize,
	})
	presentation := commandPresentation{Text: telegramPersonaText(view.Text)}
	switch view.Screen {
	case controlplane.ScreenPicker:
		presentation.ReplyMarkup = pickerKeyboardView(*result.Picker, view)
	case controlplane.ScreenForm:
		presentation.ReplyMarkup = formKeyboard(*result.Form)
	case controlplane.ScreenConfirm:
		presentation.ReplyMarkup = confirmKeyboard(*result.Confirm)
	case controlplane.ScreenInfo:
		presentation.ReplyMarkup = infoKeyboard(view.Footer)
	}
	return presentation
}
