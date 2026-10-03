package telegram

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	callbackKindCommand = "confirm"
	callbackKindDismiss = "dismiss"
)

func commandCallbackData(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return cbPicker + string(callbackKindDismiss) + "::"
	}
	return cbPicker + string(callbackKindCommand) + ":" + url.QueryEscape(command) + ":"
}

// pickerPageCallbackData shows page of the picker that command shows.
func pickerPageCallbackData(command string, page int) string {
	return cbPickerPage + url.QueryEscape(strings.TrimSpace(command)) + ":" + fmt.Sprint(page)
}

func parsePickerCallbackData(data string) (string, string, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(data, cbPicker))
	parts := strings.SplitN(payload, ":", 3)
	if len(parts) != 3 {
		return "", "", false
	}
	command, ok := unescapeCallbackPart(parts[1])
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), command, true
}

func parsePickerPageCallbackData(data string) (string, int, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(data, cbPickerPage))
	escaped, rawPage, ok := strings.Cut(payload, ":")
	if !ok {
		return "", 0, false
	}
	page, err := strconv.Atoi(strings.TrimSpace(rawPage))
	if err != nil {
		return "", 0, false
	}
	command, ok := unescapeCallbackPart(escaped)
	return command, page, ok
}

func unescapeCallbackPart(value string) (string, bool) {
	decoded, err := url.QueryUnescape(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	return decoded, true
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
	if data == "" || len([]byte(data)) <= maxCallbackDataBytes {
		return data
	}
	ref := callbackRefData(data)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callbacks.put(ref, data)
	return ref
}

func (w *Worker) resolveCallbackData(data string) string {
	if !strings.HasPrefix(data, cbCallbackRef) {
		return data
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	resolved, _ := w.callbacks.get(data)
	if resolved == "" {
		return data
	}
	return resolved
}

func callbackRefData(data string) string {
	sum := sha256.Sum256([]byte(data))
	token := base64.RawURLEncoding.EncodeToString(sum[:12])
	return cbCallbackRef + token
}
