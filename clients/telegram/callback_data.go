package telegram

import (
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
