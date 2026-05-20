package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
)

func TestConfirmCommandLoadingOnlyCancels(t *testing.T) {
	dialog := NewConfirmCommand(surfacecommon.DefaultCommon(), ConfirmCommandData{
		Message:        "Download engine?",
		ConfirmLabel:   "Download",
		CancelLabel:    "Cancel",
		ConfirmCommand: "/modules voice tts provider-action piper install-runtime-confirm",
		CancelCommand:  "/modules voice tts provider piper",
	})
	_ = dialog.StartLoading()

	if action := dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}); action != nil {
		t.Fatalf("enter while loading action = %T, want nil", action)
	}

	dialog = NewConfirmCommand(surfacecommon.DefaultCommon(), ConfirmCommandData{
		Message:        "Download engine?",
		ConfirmLabel:   "Download",
		CancelLabel:    "Cancel",
		ConfirmCommand: "/modules voice tts provider-action piper install-runtime-confirm",
		CancelCommand:  "/modules voice tts provider piper",
	})
	_ = dialog.StartLoading()
	if action := dialog.HandleMsg(tea.KeyPressMsg{Code: 'y', Text: "y"}); action != nil {
		t.Fatalf("y while loading action = %T, want nil", action)
	}
	if action := dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEsc}); action != nil {
		t.Fatalf("esc while loading action = %T, want nil", action)
	}
}
