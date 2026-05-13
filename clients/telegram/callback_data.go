package telegram

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

const (
	callbackKindCommand controlplane.PickerKind = "confirm"
	callbackKindDismiss controlplane.PickerKind = "dismiss"
)

func commandCallbackData(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return cbPicker + string(callbackKindDismiss) + "::"
	}
	return cbPicker + string(callbackKindCommand) + ":" + url.QueryEscape(command) + ":"
}

func pickerPageCallbackData(kind controlplane.PickerKind, contextID string, page int) string {
	return cbPickerPage + string(kind) + ":" + url.QueryEscape(strings.TrimSpace(contextID)) + ":" + fmt.Sprint(page)
}

func parsePickerCallbackData(data string) (controlplane.PickerKind, string, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(data, cbPicker))
	parts := strings.SplitN(payload, ":", 3)
	if len(parts) != 3 {
		return "", "", false
	}
	command, ok := unescapeCallbackPart(parts[1])
	if !ok {
		return "", "", false
	}
	return controlplane.PickerKind(strings.TrimSpace(parts[0])), command, true
}

func parsePickerPageCallbackData(data string) (controlplane.PickerKind, string, int, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(data, cbPickerPage))
	parts := strings.SplitN(payload, ":", 3)
	if len(parts) != 3 {
		return "", "", 0, false
	}
	page, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		return "", "", 0, false
	}
	contextID, ok := unescapeCallbackPart(parts[1])
	if !ok {
		return "", "", 0, false
	}
	return controlplane.PickerKind(strings.TrimSpace(parts[0])), contextID, page, true
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
	if w.callbacks == nil {
		w.callbacks = map[string]string{}
	}
	w.callbacks[ref] = data
	return ref
}

func (w *Worker) resolveCallbackData(data string) string {
	if !strings.HasPrefix(data, cbCallbackRef) {
		return data
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	resolved := w.callbacks[data]
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
