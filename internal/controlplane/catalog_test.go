package controlplane

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
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
		{kind: PickerModules, itemID: "tts", want: "/modules tts"},
		{kind: PickerModules, itemID: "stt", want: "/modules stt"},
		{kind: PickerModules, itemID: "agents", want: "/modules agents"},
		{kind: PickerTextToSpeech, itemID: "enabled", want: "/modules tts enabled"},
		{kind: PickerTextToSpeech, itemID: "provider", want: "/modules tts provider"},
		{kind: PickerSpeechToText, itemID: "enabled", want: "/modules stt enabled"},
		{kind: PickerSpeechToText, itemID: "provider", want: "/modules stt provider"},
		{kind: PickerVoiceEnabled, contextID: "tts", itemID: "yes", want: "/modules tts set-enabled yes"},
		{kind: PickerVoiceProvider, contextID: "stt", itemID: "whisper", want: "/modules stt provider whisper"},
		{kind: PickerExternalAgents, itemID: "codex-app", want: "/modules agents codex-app"},
		{kind: PickerExternalAgent, contextID: "codex-app", itemID: "path", want: "/modules agents codex-app path"},
		{kind: PickerExternalAgent, contextID: "codex-app", itemID: "enabled", want: "/modules agents codex-app enabled"},
		{kind: PickerExternalAgent, contextID: "codex-app", itemID: "new", want: "/session new codex-app"},
		{kind: PickerExternalAgentOn, contextID: "codex-app", itemID: "no", want: "/modules agents codex-app set-enabled no"},
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

func TestProviderCommandBuildersEncodeIDsAndPreserveValues(t *testing.T) {
	providerID := "local ai"
	token := "form-token"

	tests := map[string]string{
		"provider":        providerCommand(),
		"provider edit":   providerEditCommand(providerID),
		"provider field":  providerEditFieldCommand("model", providerID, token),
		"provider set":    providerEditSetCommand("model", providerID, token, "gpt-5.4 nano"),
		"provider prefix": providerEditSetCommandPrefix("key", providerID, token),
		"provider key":    providerKeyCommandPrefix(providerID),
		"custom delete":   customProviderCommand("delete", providerEncodedID(providerID)),
	}

	want := map[string]string{
		"provider":        "/provider",
		"provider edit":   "/provider edit local+ai",
		"provider field":  "/provider edit field model local+ai form-token",
		"provider set":    "/provider edit set model local+ai form-token gpt-5.4 nano",
		"provider prefix": "/provider edit set key local+ai form-token ",
		"provider key":    "/provider key local+ai ",
		"custom delete":   "/provider custom delete local+ai",
	}
	for name, got := range tests {
		if got != want[name] {
			t.Fatalf("%s command = %q, want %q", name, got, want[name])
		}
	}
}

func TestFirstCommandStepRequiresWholeVerb(t *testing.T) {
	step, rest := firstCommandStep("custom openai form-token")
	if step != "custom" || rest != "openai form-token" {
		t.Fatalf("firstCommandStep custom = (%q, %q), want custom/openai form-token", step, rest)
	}
	step, rest = firstCommandStep("customize")
	if step != "customize" || rest != "" {
		t.Fatalf("firstCommandStep customize = (%q, %q), want customize/empty", step, rest)
	}
}

func TestExternalAgentCommandBuilders(t *testing.T) {
	tests := map[string]string{
		"modules":     modulesCommand(),
		"storage":     storageCommand(),
		"tts":         textToSpeechCommand(),
		"stt":         speechToTextCommand(),
		"agents":      externalAgentsCommand(),
		"agent":       externalAgentCommand("codex-app"),
		"path":        externalAgentPathCommandPrefix("codex-app"),
		"enabled":     externalAgentEnabledCommand("codex-app"),
		"set enabled": externalAgentSetEnabledCommand("codex-app", "no"),
		"enable top":  externalAgentUpdateEnabledCommand("codex-app", true),
		"disable top": externalAgentUpdateEnabledCommand("codex-app", false),
		"new session": externalAgentNewSessionCommand("codex-app"),
	}

	want := map[string]string{
		"modules":     "/modules",
		"storage":     "/modules storage",
		"tts":         "/modules tts",
		"stt":         "/modules stt",
		"agents":      "/modules agents",
		"agent":       "/modules agents codex-app",
		"path":        "/modules agents codex-app path ",
		"enabled":     "/modules agents codex-app enabled",
		"set enabled": "/modules agents codex-app set-enabled no",
		"enable top":  "/modules agents enable codex-app",
		"disable top": "/modules agents disable codex-app",
		"new session": "/session new codex-app",
	}
	for name, got := range tests {
		if got != want[name] {
			t.Fatalf("%s command = %q, want %q", name, got, want[name])
		}
	}
}

func TestStorageCommandBuilders(t *testing.T) {
	tests := map[string]string{
		"storage":             storageCommand(),
		"import":              storageImportCommand(),
		"import prefix":       storageImportCommandPrefix(),
		"files":               storageFilesCommand(),
		"file":                storageFileCommand("docs/a.txt"),
		"read":                storageReadCommand("docs/a.txt"),
		"delete":              storageDeleteCommand("docs/a.txt"),
		"delete confirm":      storageDeleteConfirmCommand("docs/a.txt"),
		"temp":                storageTempCommand(),
		"temp file":           storageTempFileCommand("tmp/a.png"),
		"temp promote":        storageTempPromoteCommand("tmp/a.png"),
		"temp delete":         storageTempDeleteCommand("tmp/a.png"),
		"temp delete confirm": storageTempDeleteConfirmCommand("tmp/a.png"),
		"cleanup":             storageTempCleanupCommand(),
		"cleanup confirm":     storageTempCleanupConfirmCommand(),
		"cleanup mode":        storageTempCleanupModeCommand(),
		"toggle":              storageTempToggleCommand("on"),
		"days":                storageTempDaysCommand(),
		"days prefix":         storageTempDaysCommandPrefix(),
		"max":                 storageTempMaxCommand(),
		"max prefix":          storageTempMaxCommandPrefix(),
	}

	want := map[string]string{
		"storage":             "/modules storage",
		"import":              "/modules storage import",
		"import prefix":       "/modules storage import ",
		"files":               "/modules storage files",
		"file":                "/modules storage file docs/a.txt",
		"read":                "/modules storage read docs/a.txt",
		"delete":              "/modules storage delete docs/a.txt",
		"delete confirm":      "/modules storage delete-confirm docs/a.txt",
		"temp":                "/modules storage temp",
		"temp file":           "/modules storage temp-file tmp/a.png",
		"temp promote":        "/modules storage temp-promote tmp/a.png",
		"temp delete":         "/modules storage temp-delete tmp/a.png",
		"temp delete confirm": "/modules storage temp-delete-confirm tmp/a.png",
		"cleanup":             "/modules storage temp-cleanup",
		"cleanup confirm":     "/modules storage temp-cleanup-confirm",
		"cleanup mode":        "/modules storage temp-cleanup-mode",
		"toggle":              "/modules storage temp-toggle on",
		"days":                "/modules storage temp-days",
		"days prefix":         "/modules storage temp-days ",
		"max":                 "/modules storage temp-max",
		"max prefix":          "/modules storage temp-max ",
	}
	for name, got := range tests {
		if got != want[name] {
			t.Fatalf("%s command = %q, want %q", name, got, want[name])
		}
	}
}

func TestSessionCommandBuilders(t *testing.T) {
	tests := map[string]string{
		"new":              newSessionCommand(),
		"new titled":       newSessionCommand("Docs"),
		"sessions":         sessionsCommand(),
		"session new":      sessionNewCommand("matrixclaw"),
		"session menu":     sessionMenuCommand("session_1"),
		"session use":      sessionUseCommand("session_1"),
		"session rename":   sessionRenameCommand("session_1"),
		"rename prefix":    sessionRenameCommandPrefix("session_1"),
		"session delete":   sessionDeleteCommand("session_1"),
		"delete confirmed": sessionDeleteConfirmedCommand("session_1"),
	}

	want := map[string]string{
		"new":              "/new",
		"new titled":       "/new Docs",
		"sessions":         "/sessions",
		"session new":      "/session new matrixclaw",
		"session menu":     "/session menu session_1",
		"session use":      "/session use session_1",
		"session rename":   "/session rename session_1",
		"rename prefix":    "/session rename session_1 ",
		"session delete":   "/session delete session_1",
		"delete confirmed": "/session delete-confirmed session_1",
	}
	for name, got := range tests {
		if got != want[name] {
			t.Fatalf("%s command = %q, want %q", name, got, want[name])
		}
	}
}

func TestTaskCommandBuilders(t *testing.T) {
	tests := map[string]string{
		"tasks":                 tasksCommand(),
		"archive":               tasksArchiveCommand(),
		"menu":                  taskMenuCommand("job_1"),
		"pause":                 taskPauseCommand("job_1"),
		"resume":                taskResumeCommand("job_1"),
		"complete":              taskCompleteCommand("job_1"),
		"delete":                taskDeleteCommand("job_1"),
		"delete confirm":        taskDeleteConfirmCommand("job_1"),
		"delete closed":         tasksDeleteClosedCommand(),
		"delete closed confirm": tasksDeleteClosedConfirmCommand(),
		"run":                   taskRunCommand("job_1"),
	}

	want := map[string]string{
		"tasks":                 "/tasks",
		"archive":               "/tasks archive",
		"menu":                  "/tasks menu job_1",
		"pause":                 "/tasks pause job_1",
		"resume":                "/tasks resume job_1",
		"complete":              "/tasks complete job_1",
		"delete":                "/tasks delete job_1",
		"delete confirm":        "/tasks delete-confirm job_1",
		"delete closed":         "/tasks delete-closed",
		"delete closed confirm": "/tasks delete-closed-confirm",
		"run":                   "/tasks run job_1",
	}
	for name, got := range tests {
		if got != want[name] {
			t.Fatalf("%s command = %q, want %q", name, got, want[name])
		}
	}
}

func TestCoreCommandBuilders(t *testing.T) {
	tests := map[string]string{
		"permissions":     permissionsCommand(),
		"permission mode": permissionsCommand("full_auto"),
		"context":         contextCommand(),
		"context info":    contextInfoCommand(),
		"context compact": contextCompactCommand(),
		"context confirm": contextCompactConfirmCommand(),
		"server":          serverCommand(),
		"status":          statusCommand(),
		"restart":         restartCommand(),
		"restart confirm": restartConfirmCommand(),
	}

	want := map[string]string{
		"permissions":     "/permissions",
		"permission mode": "/permissions full_auto",
		"context":         "/context",
		"context info":    "/context info",
		"context compact": "/context compact",
		"context confirm": "/context compact confirm",
		"server":          "/server",
		"status":          "/status",
		"restart":         "/restart",
		"restart confirm": "/restart confirm",
	}
	for name, got := range tests {
		if got != want[name] {
			t.Fatalf("%s command = %q, want %q", name, got, want[name])
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
	if len(items) != 9 {
		t.Fatalf("len(items) = %d, want 9", len(items))
	}
	byCommand := make(map[string]CommandView, len(items))
	for _, item := range items {
		byCommand[item.Command] = item
	}
	for _, command := range []string{"/new", "/sessions", "/provider", "/permissions", "/context", "/plan", "/modules", "/tasks", "/server"} {
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

func TestBuildCommandViewMarksMatrixclawOnlyCommandsForExternalAgent(t *testing.T) {
	views := BuildCommandView(MenuState{
		SessionTitle: "Codex work",
		Capabilities: core.SessionCapabilities{ExternalAgent: true},
	})
	byCommand := make(map[string]CommandView, len(views))
	for _, item := range views {
		byCommand[item.Command] = item
	}
	for _, command := range []string{"/provider", "/permissions", "/plan"} {
		item := byCommand[command]
		if !item.Disabled || item.Status != "Matrixclaw only" {
			t.Fatalf("%s item = %#v, want disabled Matrixclaw only", command, item)
		}
	}
	if byCommand["/sessions"].Disabled || byCommand["/modules"].Disabled {
		t.Fatalf("shared commands should stay enabled: sessions=%#v modules=%#v", byCommand["/sessions"], byCommand["/modules"])
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
