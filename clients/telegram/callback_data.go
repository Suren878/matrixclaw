package telegram

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

// A button runs a controlplane command line, which keeps every operand one
// token: "pk:<command>" runs it ("pk:" alone closes the menu) and
// "pg:<page>:<command>" shows a page of the picker it shows. Data over
// Telegram's 64 bytes travels as "rf:<hash>", resolved from w.callbacks, which
// is kept on disk so menus sent before a restart still work.

func commandCallbackData(command string) string {
	return cbPicker + strings.TrimSpace(command)
}

// pickerPageCallbackData shows page of the picker that command shows.
func pickerPageCallbackData(command string, page int) string {
	return cbPickerPage + strconv.Itoa(page) + ":" + strings.TrimSpace(command)
}

// parsePickerCallbackData is the command a button runs, "" for one that
// closes the menu.
func parsePickerCallbackData(data string) string {
	return strings.TrimSpace(strings.TrimPrefix(data, cbPicker))
}

func parsePickerPageCallbackData(data string) (string, int, bool) {
	rawPage, command, ok := strings.Cut(strings.TrimPrefix(data, cbPickerPage), ":")
	page, err := strconv.Atoi(rawPage)
	if !ok || err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(command), page, true
}

func (w *Worker) compactInlineKeyboardMarkup(markup *InlineKeyboardMarkup) *InlineKeyboardMarkup {
	if markup == nil {
		return nil
	}
	rows := make([][]InlineKeyboardButton, len(markup.InlineKeyboard))
	changed := false
	for rowIndex, row := range markup.InlineKeyboard {
		rows[rowIndex] = make([]InlineKeyboardButton, len(row))
		for buttonIndex, button := range row {
			compact := w.compactCallbackData(button.CallbackData)
			if compact != button.CallbackData {
				changed = true
				button.CallbackData = compact
			}
			rows[rowIndex][buttonIndex] = button
		}
	}
	if !changed {
		return markup
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (w *Worker) compactCallbackData(data string) string {
	if len(data) <= maxCallbackDataBytes {
		return data
	}
	sum := sha256.Sum256([]byte(data))
	ref := cbCallbackRef + base64.RawURLEncoding.EncodeToString(sum[:12])
	w.callbacks.put(ref, data)
	return ref
}

// resolveCallbackData is the data a short reference stands for; data that is
// no reference, or one no longer kept, is returned as is.
func (w *Worker) resolveCallbackData(data string) string {
	if !strings.HasPrefix(data, cbCallbackRef) {
		return data
	}
	if resolved, ok := w.callbacks.get(data); ok {
		return resolved
	}
	return data
}
