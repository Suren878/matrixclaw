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

func TestDisabledPickerRowsKeepTheMenu(t *testing.T) {
	picker := controlplane.PickerData{Kind: controlplane.PickerModule, Title: "Telephony", Close: "/modules"}
	picker.Items = []controlplane.PickerItem{{ID: "enabled", Title: "Enabled", Disabled: true}}

	_, markup := presentPicker(picker, 0)

	data := markup.InlineKeyboard[0][0].CallbackData
	if kind, _, ok := parsePickerCallbackData(data); strings.HasPrefix(data, cbPicker) && ok && kind == callbackKindDismiss {
		t.Fatalf("a disabled row dismisses the menu: %q", data)
	}
}

func TestInfoShowsEachValueOnce(t *testing.T) {
	info := controlplane.InfoData{Title: "Run Budget", Text: "Steps: 50 (default)\n\nUse /budget steps N.", Rows: []controlplane.InfoRow{{Label: "Steps", Value: "50 (default)"}}}
	if text := infoText(info); strings.Count(text, "Steps: 50") != 1 {
		t.Fatalf("info text = %q", text)
	}
	rowsOnly := controlplane.InfoData{Title: "Server Status", Rows: []controlplane.InfoRow{{Label: "Uptime", Value: "1h"}}}
	if text := infoText(rowsOnly); text != "Server Status\nUptime: 1h" {
		t.Fatalf("rows only info text = %q", text)
	}
}
