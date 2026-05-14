package controlplane

import (
	"strings"
	"testing"
)

func TestParseRecognizesPrimaryCommandsAndAliases(t *testing.T) {
	tests := []struct {
		input string
		id    CommandID
		args  string
		ok    bool
	}{
		{input: "/new docs", id: CommandNewSession, args: "docs", ok: true},
		{input: "/sessions", id: CommandSessions, ok: true},
		{input: "/session use session_1", id: CommandSession, args: "use session_1", ok: true},
		{input: "/session current", id: CommandSession, args: "current", ok: true},
		{input: "/mode full", id: CommandPermissions, args: "full", ok: true},
		{input: "/commands", id: CommandHelp, ok: true},
		{input: "/start", id: CommandHelp, ok: true},
		{input: "hello", ok: false},
	}

	for _, tt := range tests {
		spec, args, ok := Parse(tt.input)
		if ok != tt.ok {
			t.Fatalf("Parse(%q) ok=%v want %v", tt.input, ok, tt.ok)
		}
		if !tt.ok {
			continue
		}
		if spec.ID != tt.id {
			t.Fatalf("Parse(%q) id=%q want %q", tt.input, spec.ID, tt.id)
		}
		if args != tt.args {
			t.Fatalf("Parse(%q) args=%q want %q", tt.input, args, tt.args)
		}
	}
}

func TestPickerCommandBuildsSharedCommands(t *testing.T) {
	tests := []struct {
		kind      PickerKind
		contextID string
		itemID    string
		want      string
	}{
		{kind: PickerSessions, itemID: "new", want: "/new"},
		{kind: PickerSessions, itemID: "cancel", want: ""},
		{kind: PickerSessions, itemID: "session_1", want: "/session menu session_1"},
		{kind: PickerSessionRuntime, itemID: "matrixclaw", want: "/session new matrixclaw"},
		{kind: PickerSessionRuntime, itemID: "codex", want: "/session new codex"},
		{kind: PickerSessionRuntime, itemID: "back", want: "/sessions"},
		{kind: PickerSessionActions, contextID: "session_1", itemID: "use", want: "/session use session_1"},
		{kind: PickerSessionActions, contextID: "session_1", itemID: "delete", want: "/session delete session_1"},
		{kind: PickerProvider, itemID: "openai", want: "/provider openai"},
		{kind: PickerProvider, itemID: "custom", want: "/provider custom"},
		{kind: PickerProvider, itemID: "cancel", want: ""},
		{kind: PickerProviderCustom, itemID: "openai", want: "/provider custom openai"},
		{kind: PickerProviderActions, contextID: "local-ai", itemID: "edit", want: "/provider edit local-ai"},
		{kind: PickerProviderActions, contextID: "local-ai", itemID: "delete", want: "/provider custom delete local-ai"},
		{kind: PickerPermissions, itemID: "full_auto", want: "/permissions full_auto"},
		{kind: PickerModules, itemID: "storage", want: "/modules storage"},
		{kind: PickerStorageTemp, itemID: "toggle", want: "/modules storage temp-cleanup-mode"},
		{kind: PickerStorageCleanup, itemID: "on", want: "/modules storage temp-toggle on"},
		{kind: PickerStorageFiles, itemID: "file:docs/a.txt", want: "/modules storage file docs/a.txt"},
		{kind: PickerStorageFile, contextID: "docs/a.txt", itemID: "delete", want: "/modules storage delete docs/a.txt"},
		{kind: PickerServer, itemID: "status", want: "/status"},
		{kind: PickerServer, itemID: "restart", want: "/restart"},
		{kind: PickerTasks, itemID: "open:job_1", want: "/tasks menu job_1"},
		{kind: PickerTasks, itemID: "archive", want: "/tasks archive"},
		{kind: PickerTaskActions, contextID: "job_1", itemID: "run", want: "/tasks run job_1"},
		{kind: PickerTaskActions, contextID: "job_1", itemID: "archive", want: "/tasks complete job_1"},
		{kind: PickerTaskActions, contextID: "job_1", itemID: "delete", want: "/tasks delete job_1"},
		{kind: PickerTaskArchive, itemID: "delete_closed", want: "/tasks delete-closed"},
		{kind: PickerTaskArchive, itemID: "closed:job_2", want: "/tasks menu job_2"},
		{kind: PickerSessions, want: "/sessions"},
	}

	for _, tt := range tests {
		if got := PickerCommandFor(tt.kind, tt.contextID, tt.itemID); got != tt.want {
			t.Fatalf("PickerCommandFor(%q, %q, %q)=%q want %q", tt.kind, tt.contextID, tt.itemID, got, tt.want)
		}
	}
}

func TestBuildCommandViewMarksMenuItems(t *testing.T) {
	views := BuildCommandView(MenuState{
		SessionTitle: "Docs",
		ProviderID:   "openai",
		ModelID:      "gpt-5.4",
	})
	items := make([]CommandView, 0, len(views))
	for _, view := range views {
		if view.Menu {
			items = append(items, view)
		}
	}
	if len(items) != 8 {
		t.Fatalf("len(items) = %d, want 8", len(items))
	}
	byCommand := make(map[string]CommandView, len(items))
	for _, item := range items {
		byCommand[item.Command] = item
	}
	for _, command := range []string{"/new", "/sessions", "/provider", "/permissions", "/context", "/modules", "/tasks", "/server"} {
		if _, ok := byCommand[command]; !ok {
			t.Fatalf("menu items missing %s: %#v", command, items)
		}
	}
	if _, ok := byCommand["/model"]; ok {
		t.Fatalf("menu items should configure models through providers now: %#v", items)
	}
	if byCommand["/sessions"].Status != "Docs" {
		t.Fatalf("menu status split mismatch: %#v", items)
	}
	if byCommand["/server"].Group != MenuItemGroupSecondary {
		t.Fatalf("menu group mismatch: server=%#v", byCommand["/server"])
	}
}

func TestPickerItemRoleHelpers(t *testing.T) {
	tests := []struct {
		item      PickerItem
		cancel    bool
		back      bool
		danger    bool
		action    bool
		separated bool
	}{
		{item: PickerItem{Role: PickerItemRoleCancel}, cancel: true, separated: true},
		{item: PickerItem{Role: PickerItemRoleBack}, back: true, separated: true},
		{item: PickerItem{Role: PickerItemRoleDanger}, danger: true, separated: true},
		{item: PickerItem{Role: PickerItemRoleAction}, action: true, separated: true},
		{item: PickerItem{}, separated: false},
	}
	for _, tt := range tests {
		if tt.item.IsCancel() != tt.cancel || tt.item.IsBack() != tt.back || tt.item.IsDanger() != tt.danger || tt.item.IsAction() != tt.action || tt.item.NeedsSeparator() != tt.separated {
			t.Fatalf("role helpers for %#v mismatch", tt.item)
		}
	}
}

func TestHelpTextShowsOnlyPublicCommands(t *testing.T) {
	text := HelpText()
	if strings.Contains(text, "/new") || strings.Contains(text, "/session -") || strings.Contains(text, "/status") || strings.Contains(text, "/restart") {
		t.Fatalf("HelpText() unexpectedly includes hidden commands: %q", text)
	}
	for _, command := range []string{"/sessions", "/provider", "/permissions", "/server", "/help"} {
		if !strings.Contains(text, command) {
			t.Fatalf("HelpText() missing %s: %q", command, text)
		}
	}
	if strings.Contains(text, "/model") {
		t.Fatalf("HelpText() should not expose separate model setup: %q", text)
	}
}
