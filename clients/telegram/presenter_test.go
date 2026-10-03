package telegram

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func TestLongPickersPageWithTheirCommand(t *testing.T) {
	picker := controlplane.PickerData{Kind: controlplane.PickerSessions, Title: "Sessions", Command: "/sessions", Back: "/help"}
	for i := range 45 {
		picker.Items = append(picker.Items, controlplane.PickerItem{ID: fmt.Sprint(i), Title: fmt.Sprint("s", i), Command: "/session menu " + fmt.Sprint(i), Selected: i == 30})
	}

	_, markup := presentPicker(picker, -1)

	rows := markup.InlineKeyboard
	if first := rows[0][0].Text; first != "💬 s20" {
		t.Fatalf("first row on the selected row's page = %q", first)
	}
	if selected := rows[10][0].Text; selected != "✅ 💬 s30" {
		t.Fatalf("selected row = %q", selected)
	}
	nav := rows[len(rows)-2]
	if len(nav) != 3 || nav[1].Text != "2/3" || nav[2].CallbackData != pickerPageCallbackData("/sessions", 2) {
		t.Fatalf("page buttons = %+v", nav)
	}
	if command, page, ok := parsePickerPageCallbackData(nav[2].CallbackData); !ok || command != "/sessions" || page != 2 {
		t.Fatalf("page callback = %q %d %v", command, page, ok)
	}
	if back := rows[len(rows)-1][0]; !strings.Contains(back.Text, "Back") {
		t.Fatalf("last row = %+v", back)
	}

	picker.Command = ""
	if _, markup := presentPicker(picker, 0); len(markup.InlineKeyboard) != 46 {
		t.Fatalf("a picker without a command shows %d rows, want all 45 and Back", len(markup.InlineKeyboard))
	}
}
