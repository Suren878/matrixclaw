package runtime

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	surfaceinput "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/input"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSubmitMsgWithoutSessionRestoresAttachments(t *testing.T) {
	model := newApp(nil, nil)
	attachment := surfaceeditor.Attachment{
		FilePath: "notes.txt",
		FileName: "notes.txt",
		MimeType: "text/plain",
		Content:  []byte("hello"),
	}

	next, cmd := model.Update(surfaceinput.SubmitMsg{
		Content:     "fix this",
		Attachments: []surfaceeditor.Attachment{attachment},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no command when session is missing")
	}
	if got := model.input.Editor().Value(); got != "fix this" {
		t.Fatalf("editor value = %q, want restored draft", got)
	}
	if got := model.input.Editor().Attachments(); len(got) != 1 || got[0].FileName != "notes.txt" {
		t.Fatalf("attachments restored = %#v, want notes.txt", got)
	}
}

func TestSubmitMsgWithoutRuntimeRestoresDraftAndAttachments(t *testing.T) {
	model := newApp(nil, nil)
	model.session = "session-1"
	attachment := surfaceeditor.Attachment{
		FilePath: "/tmp/a.txt",
		FileName: "a.txt",
		MimeType: "text/plain",
		Content:  []byte("hello"),
	}

	next, cmd := model.Update(surfaceinput.SubmitMsg{
		Content:     "hello",
		Attachments: []surfaceeditor.Attachment{attachment},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected follow-up result command")
	}

	msg := cmd()
	if msg == nil {
		t.Fatal("expected send result message")
	}

	next, cmd = model.Update(msg)
	if next == nil {
		t.Fatal("expected model after send result")
	}
	if cmd != nil {
		t.Fatal("expected no follow-up command after local failure")
	}
	if got := model.input.Editor().Value(); got != "hello" {
		t.Fatalf("editor value = %q, want %q", got, "hello")
	}
	attachments := model.input.Editor().Attachments()
	if got := len(attachments); got != 1 {
		t.Fatalf("len(attachments) = %d, want 1", got)
	}
	if attachments[0].FileName != attachment.FileName {
		t.Fatalf("attachment filename = %q, want %q", attachments[0].FileName, attachment.FileName)
	}
	if !strings.Contains(model.err, "terminal runtime is not configured") {
		t.Fatalf("err = %q, want runtime not configured", model.err)
	}
	if model.busy {
		t.Fatal("expected busy to reset after local send failure")
	}
}

func TestControlplaneFinalResultClosesDialogStack(t *testing.T) {
	model := newApp(nil, nil)
	model.dialog.OpenDialog(surfacedialog.NewCommands(model.com, surfacedialog.CommandsData{}))

	next, cmd := model.Update(controlplaneResultMsg{
		result: controlplane.Result{Handled: true, Text: "Updated"},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no follow-up command")
	}
	if !model.dialog.ContainsDialog(surfacedialog.InfoID) {
		t.Fatal("expected final controlplane result to open info dialog")
	}
}

func TestServerStatusActionOpensLiveDialog(t *testing.T) {
	model := newApp(nil, nil)
	model.rt = &Runtime{}
	model.dialog.OpenDialog(surfacedialog.NewCommands(model.com, surfacedialog.CommandsData{}))

	next, cmd := model.Update(surfacedialog.ActionRunControlplaneCommand{Command: "/status"})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected status refresh command")
	}
	if !model.dialog.ContainsDialog(surfacedialog.ServerStatusInfoID) {
		t.Fatal("expected server status dialog")
	}

	next, cmd = model.Update(serverStatusRefreshMsg{text: controlplane.FormatServerStatus(core.ServerStatus{UptimeSeconds: 2, ProcessRSSBytes: 1024})})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no command after status refresh")
	}
}

func TestContextCompactActions(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		wantInfo bool
	}{
		{name: "waits for confirm", command: "/context compact"},
		{name: "confirmed shows progress", command: "/context compact confirm", wantInfo: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newApp(nil, nil)
			model.dialog.OpenDialog(surfacedialog.NewPicker(model.com, surfacedialog.PickerData{ID: surfacedialog.PickerID, Title: "Context"}))

			next, cmd := model.Update(surfacedialog.ActionRunControlplaneCommand{Command: tt.command})
			if next == nil {
				t.Fatal("expected model")
			}
			if cmd == nil {
				t.Fatal("expected controlplane command")
			}
			if model.err != "" {
				t.Fatalf("err = %q, want empty progress state", model.err)
			}
			if got := model.dialog.ContainsDialog(surfacedialog.InfoID); got != tt.wantInfo {
				t.Fatalf("info dialog open = %v, want %v", got, tt.wantInfo)
			}
			if tt.wantInfo && model.dialog.ContainsDialog(surfacedialog.PickerID) {
				t.Fatal("expected context picker to close")
			}
		})
	}
}

func TestServerRestartActionOpensProgressDialog(t *testing.T) {
	model := newApp(nil, nil)
	model.rt = &Runtime{}
	model.dialog.OpenDialog(surfacedialog.NewPicker(model.com, surfacedialog.PickerData{ID: surfacedialog.PickerID, Title: "Server"}))

	next, cmd := model.Update(surfacedialog.ActionRunControlplaneCommand{Command: "/restart confirm"})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected restart command")
	}
	if !model.dialog.ContainsDialog(surfacedialog.ServerRestartInfoID) {
		t.Fatal("expected restart progress info dialog")
	}
	if model.dialog.ContainsDialog(surfacedialog.PickerID) {
		t.Fatal("expected server picker to close")
	}
}

func TestDeliveryDisplayTextUsesSummaryOrFallback(t *testing.T) {
	delivery := core.ClientDelivery{
		Summary: "Daemon restarted.",
	}
	if got := deliveryDisplayText(delivery, "fallback"); got != "Daemon restarted." {
		t.Fatalf("deliveryDisplayText() = %q, want summary", got)
	}

	delivery.Summary = ""
	if got := deliveryDisplayText(delivery, "fallback"); got != "fallback" {
		t.Fatalf("deliveryDisplayText() = %q, want fallback", got)
	}
}

func TestControlplanePickerOpensGenericPickerDialog(t *testing.T) {
	model := newApp(nil, nil)

	next, cmd := model.Update(controlplaneResultMsg{
		result: controlplane.Result{
			Picker: &controlplane.PickerData{
				Kind:  controlplane.PickerSessions,
				Title: "Sessions",
				Items: []controlplane.PickerItem{
					{ID: "new", Title: "Create New"},
					{ID: "session-1", Title: "Docs", Selected: true},
				},
			},
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no follow-up command")
	}
	if !model.dialog.ContainsDialog(surfacedialog.PickerID) {
		t.Fatal("expected generic picker dialog to open")
	}
}

func TestEscFromCommandRootPickerReturnsToCommands(t *testing.T) {
	model := newApp(nil, nil)
	model.dialog.OpenDialog(surfacedialog.NewCommands(model.com, surfacedialog.CommandsData{}))

	next, cmd := model.Update(surfacedialog.ActionRunControlplaneCommand{Command: "/sessions"})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected controlplane command")
	}

	next, cmd = model.Update(controlplaneResultMsg{
		result: controlplane.Result{
			Picker: &controlplane.PickerData{
				Kind:  controlplane.PickerSessions,
				Title: "Sessions",
				Items: []controlplane.PickerItem{{ID: "session-1", Title: "Docs"}},
			},
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no command after picker result")
	}
	if !model.dialog.ContainsDialog(surfacedialog.PickerID) {
		t.Fatal("expected sessions picker")
	}

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no command when returning to commands")
	}
	if model.dialog.ContainsDialog(surfacedialog.PickerID) {
		t.Fatal("expected picker to close")
	}
	if !model.dialog.ContainsDialog(surfacedialog.CommandsID) {
		t.Fatal("expected commands dialog to reopen")
	}
}

func TestControlplanePickerClosesStalePrompt(t *testing.T) {
	model := newApp(nil, nil)
	model.dialog.OpenDialog(surfacedialog.NewPromptCommand(model.com, surfacedialog.PromptCommandData{
		Title:               "Max size",
		SubmitCommandPrefix: "/modules storage temp-max ",
		CancelCommand:       "/modules storage temp",
	}))

	next, cmd := model.Update(controlplaneResultMsg{
		result: controlplane.Result{
			Picker: &controlplane.PickerData{
				Kind:  controlplane.PickerStorageTemp,
				Title: "Temporary Files",
				Items: []controlplane.PickerItem{{ID: "back", Title: "Back", Role: controlplane.PickerItemRoleBack}},
			},
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no follow-up command")
	}
	if model.dialog.ContainsDialog(surfacedialog.PromptCommandID) {
		t.Fatal("expected stale prompt to close")
	}
	if !model.dialog.ContainsDialog(surfacedialog.PickerID) {
		t.Fatal("expected picker dialog to open")
	}
	if model.dialog.ContainsDialog(surfacedialog.CommandsID) {
		t.Fatal("expected commands dialog to close")
	}
}

func TestPreviewActionsOpenDialogs(t *testing.T) {
	tests := []struct {
		name     string
		msg      tea.Msg
		dialogID string
	}{
		{
			name: "diff preview",
			msg: surfacedialog.ActionOpenDiffPreview{Data: surfacedialog.DiffPreviewData{
				Title:      "Write Changes",
				FilePath:   "internal/api/server.go",
				OldContent: "package api\n",
				NewContent: "package api\nfunc Serve() {}\n",
				Additions:  1,
			}},
			dialogID: surfacedialog.DiffPreviewID,
		},
		{
			name: "file preview",
			msg: surfacedialog.ActionOpenFilePreview{Data: surfacedialog.FilePreviewData{
				Title:    "Read File",
				FilePath: "internal/api/server.go",
				Content:  "package api\n",
			}},
			dialogID: surfacedialog.FilePreviewID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newApp(nil, nil)

			next, cmd := model.Update(tt.msg)
			if next == nil {
				t.Fatal("expected model")
			}
			if cmd != nil {
				t.Fatal("expected no follow-up command")
			}
			if !model.dialog.ContainsDialog(tt.dialogID) {
				t.Fatalf("expected %s dialog to open", tt.dialogID)
			}
		})
	}
}

func TestAddImageMsgCreatesAttachmentFromExistingFilePath(t *testing.T) {
	model := newApp(nil, nil)
	model.focus = appFocusEditor

	filePath := t.TempDir() + "/notes.txt"
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	model.input.Editor().SetValue(filePath)

	next, cmd := model.Update(surfaceinput.AddImageMsg{})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no command")
	}
	attachments := model.input.Editor().Attachments()
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if attachments[0].FilePath != filePath {
		t.Fatalf("attachment path = %q, want %q", attachments[0].FilePath, filePath)
	}
	if got := model.input.Editor().Value(); got != "" {
		t.Fatalf("editor value = %q, want cleared path input", got)
	}
}
