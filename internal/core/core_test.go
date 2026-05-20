package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/orchestration"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestCoreAcceptRunAndCompleteMessageFlow(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil)
	ctx := context.Background()

	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Docs"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "hello",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	if accepted.Run.ID == "" {
		t.Fatalf("AcceptRun().Run.ID is empty")
	}
	if accepted.Run.Status != core.RunStatusAccepted {
		t.Fatalf("AcceptRun().Run.Status = %q, want %q", accepted.Run.Status, core.RunStatusAccepted)
	}

	run := waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)
	if run.Status != core.RunStatusCompleted {
		t.Fatalf("GetRun().Status = %q, want %q", run.Status, core.RunStatusCompleted)
	}

	messages, err := app.ListMessages(ctx, session.ID, 10)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(ListMessages()) = %d, want 2", len(messages))
	}
	if messages[1].RunID != accepted.Run.ID {
		t.Fatalf("assistant message RunID = %q, want %q", messages[1].RunID, accepted.Run.ID)
	}
	if messages[1].Role != core.MessageRoleAssistant {
		t.Fatalf("assistant message role = %q, want %q", messages[1].Role, core.MessageRoleAssistant)
	}
}

func TestCoreExecutesExternalAgentAttachment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openCoreTestStore(t)
	runtime := &externalRuntimeStub{
		events: []externalagents.Event{
			{Kind: externalagents.EventMessageDelta, Text: "hello from codex"},
			{Kind: externalagents.EventTurnCompleted},
		},
	}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	app := core.New(sqliteStore).WithSessionLLMs(newSessionLLMRegistryStub(newProviderStub()))
	app.WithExternalAgents(registry, sqliteStore)
	app.WithClock(func() time.Time { return time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC) })
	app.WithRunStarter(orchestration.NewStub(app))

	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Codex"})
	if err := sqliteStore.SaveExternalAgentSession(ctx, externalagents.SessionAttachment{
		SessionID:        session.ID,
		AgentID:          runtime.ID(),
		ExternalThreadID: "thread_1",
		Model:            "gpt-5.4",
		MetadataJSON:     "{}",
	}); err != nil {
		t.Fatalf("SaveExternalAgentSession() error = %v", err)
	}

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		SessionID: session.ID,
		Text:      "use codex",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	run := waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)
	if run.Status != core.RunStatusCompleted {
		t.Fatalf("run status = %q, want completed", run.Status)
	}
	if runtime.inputText.Load().(string) != "use codex" {
		t.Fatalf("runtime input = %q, want use codex", runtime.inputText.Load())
	}
	if runtime.resumeCalled.Load() {
		t.Fatal("runtime ResumeSession was called before Send; fresh external sessions should be sent directly")
	}

	messages, err := app.ListMessages(ctx, session.ID, 10)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if messages[1].Content != "hello from codex" {
		t.Fatalf("assistant content = %q, want external response", messages[1].Content)
	}
	if messages[1].Provider != runtime.ID() {
		t.Fatalf("assistant provider = %q, want %q", messages[1].Provider, runtime.ID())
	}
}

func TestCoreCreateExternalAgentSessionStartsAttachment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openCoreTestStore(t)
	runtime := &externalRuntimeStub{
		startSession: externalagents.ExternalSession{
			AgentID:           "codex-app",
			ExternalThreadID:  "thread_1",
			ExternalSessionID: "session_1",
			CWD:               "/workspace",
			Model:             "gpt-5.4",
		},
	}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	app := core.New(sqliteStore).WithSessionLLMs(newSessionLLMRegistryStub(newProviderStub()))
	app.WithExternalAgents(registry, sqliteStore)

	session, err := app.CreateSession(ctx, core.CreateSessionInput{
		Title:      "Codex",
		RuntimeID:  core.SessionRuntime("codex"),
		WorkingDir: "/workspace",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.Kind != core.SessionKindExternalAgent {
		t.Fatalf("session kind = %q, want external_agent", session.Kind)
	}
	if session.RuntimeID != core.SessionRuntimeExternalAgent {
		t.Fatalf("session runtime = %q, want external_agent", session.RuntimeID)
	}
	if session.PermissionMode != core.PermissionModeFullAuto {
		t.Fatalf("session permission mode = %q, want full_auto", session.PermissionMode)
	}
	if session.ProviderID != "" {
		t.Fatalf("external session provider_id = %q, want empty", session.ProviderID)
	}

	req := runtime.startReq.Load().(externalagents.StartSessionRequest)
	if req.CWD != "/workspace" || req.Model != "gpt-5.4" {
		t.Fatalf("StartSession request = %#v, want cwd/model", req)
	}
	if req.ApprovalPolicy != "never" || req.Sandbox != "danger-full-access" {
		t.Fatalf("StartSession policy = %q/%q, want never/danger-full-access", req.ApprovalPolicy, req.Sandbox)
	}

	attachment, err := sqliteStore.GetExternalAgentSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetExternalAgentSession() error = %v", err)
	}
	if attachment.AgentID != "codex-app" || attachment.ExternalThreadID != "thread_1" {
		t.Fatalf("attachment = %#v, want codex thread", attachment)
	}
	if attachment.ApprovalPolicy != "never" || attachment.Sandbox != "danger-full-access" {
		t.Fatalf("attachment policy = %q/%q, want never/danger-full-access", attachment.ApprovalPolicy, attachment.Sandbox)
	}
}

func TestCapabilitiesForSession(t *testing.T) {
	matrixclaw := core.CapabilitiesForSession(core.Session{Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw})
	if !matrixclaw.ProviderSelection || !matrixclaw.PermissionMode || !matrixclaw.PlanningMode || !matrixclaw.NativeTools || matrixclaw.ExternalAgent {
		t.Fatalf("matrixclaw capabilities = %#v", matrixclaw)
	}

	external := core.CapabilitiesForSession(core.Session{Kind: core.SessionKindExternalAgent, RuntimeID: core.SessionRuntimeExternalAgent})
	if external.ProviderSelection || external.PermissionMode || external.PlanningMode || external.NativeTools || !external.ExternalAgent {
		t.Fatalf("external capabilities = %#v", external)
	}
}

func TestCoreSendsAssistantProfileToProvider(t *testing.T) {
	t.Parallel()

	requests := make(chan providers.Request, 1)
	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		requests <- request
		return providers.Response{
			Text:     "profile received",
			Model:    "stub-model",
			Provider: "stub-provider",
		}, nil
	}

	app := newTestCore(t, provider)
	app.SetAssistantProfile(core.AssistantProfile{
		Name:               "Clawdia",
		SystemPrompt:       "You are matrixclaw. Base system prompt.",
		CustomInstructions: "Prefer short answers.",
	})
	ctx := context.Background()

	createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Docs"})
	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "what is your name?",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	select {
	case request := <-requests:
		if !strings.Contains(request.SystemPrompt, `configured assistant name is "Clawdia"`) {
			t.Fatalf("SystemPrompt = %q, want assistant name", request.SystemPrompt)
		}
		if !strings.Contains(request.SystemPrompt, "different assistant name") {
			t.Fatalf("SystemPrompt = %q, want assistant name precedence", request.SystemPrompt)
		}
		if !strings.Contains(request.SystemPrompt, "Base system prompt.") {
			t.Fatalf("SystemPrompt = %q, want base system prompt", request.SystemPrompt)
		}
		if request.CustomInstructions != "Prefer short answers." {
			t.Fatalf("CustomInstructions = %q, want saved custom instructions", request.CustomInstructions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider request was not captured")
	}
}

func TestCoreProviderRequestIncludesCurrentProjectRoot(t *testing.T) {
	t.Parallel()

	requests := make(chan providers.Request, 1)
	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		requests <- request
		return providers.Response{Text: "root received"}, nil
	}

	app := newTestCore(t, provider)
	ctx := context.Background()
	createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Docs"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "list files",
		WorkingDir:  "/Volumes/LVM/Downloads/project",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	select {
	case request := <-requests:
		if !strings.Contains(request.SystemPrompt, "Current project root:") {
			t.Fatalf("SystemPrompt = %q, want current project root block", request.SystemPrompt)
		}
		if !strings.Contains(request.SystemPrompt, `/Volumes/LVM/Downloads/project`) {
			t.Fatalf("SystemPrompt = %q, want working directory", request.SystemPrompt)
		}
		if !strings.Contains(request.SystemPrompt, "Resolve relative filesystem tool paths under this directory.") {
			t.Fatalf("SystemPrompt = %q, want path instruction", request.SystemPrompt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider request was not captured")
	}
}

func TestCoreSessionContextIncludesAssistantPromptAndMessages(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil)
	app.SetAssistantProfile(core.AssistantProfile{
		Name:               "Clawdia",
		SystemPrompt:       "Base system prompt.",
		CustomInstructions: "Prefer short answers.",
	})
	ctx := context.Background()
	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Docs"})
	if _, err := app.AcceptRun(ctx, core.HandleMessageInput{SessionID: session.ID, Text: "hello"}); err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}

	report, err := app.SessionContext(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionContext() error = %v", err)
	}
	if report.TokenEstimate == 0 {
		t.Fatalf("SessionContext().TokenEstimate = 0, want prompt/message estimate")
	}
	kinds := map[core.ContextBlockKind]bool{}
	for _, block := range report.Blocks {
		kinds[block.Kind] = true
	}
	for _, want := range []core.ContextBlockKind{core.ContextBlockSystemPrompt, core.ContextBlockCustomInstructions, core.ContextBlockMessages} {
		if !kinds[want] {
			t.Fatalf("SessionContext() missing block kind %q: %#v", want, report.Blocks)
		}
	}
}

func TestCoreSessionContextIncludesSelectedModelWindow(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil)
	app.SetSessionLLMs(&sessionLLMRegistryStub{
		runtime:      newProviderStub(),
		providerID:   "openai-codex",
		providerType: providers.TypeOpenAICodex,
		modelID:      "gpt-5.3-codex-spark",
	})
	ctx := context.Background()
	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Codex"})

	report, err := app.SessionContext(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionContext() error = %v", err)
	}
	if report.WindowTokens != 128_000 {
		t.Fatalf("SessionContext().WindowTokens = %d, want 128000", report.WindowTokens)
	}
}

func TestCoreSessionContextUsesManualModelWindowOverride(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil)
	app.SetSessionLLMs(&sessionLLMRegistryStub{
		runtime:       newProviderStub(),
		providerID:    "openai",
		providerType:  providers.TypeOpenAICompat,
		modelID:       "gpt-5.4",
		contextWindow: 123_456,
	})
	ctx := context.Background()
	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Manual"})

	report, err := app.SessionContext(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionContext() error = %v", err)
	}
	if report.WindowTokens != 123_456 {
		t.Fatalf("SessionContext().WindowTokens = %d, want manual override", report.WindowTokens)
	}
}

func TestEstimateMessageTokensUsesPartsAndImageWeight(t *testing.T) {
	t.Parallel()

	tokens := core.EstimateMessageTokens([]core.Message{{
		Content: "duplicate text",
		Parts: []core.MessagePart{
			{Kind: core.MessagePartKindText, Text: &core.TextPart{Text: "duplicate text"}},
			{Kind: core.MessagePartKindImage, Image: &core.ImagePart{Name: "screen.png"}},
		},
	}})
	if tokens < core.EstimatedImageTokens {
		t.Fatalf("EstimateMessageTokens() = %d, want at least image weight", tokens)
	}
	if tokens >= core.EstimatedImageTokens+core.EstimateTextTokens("duplicate text")*2 {
		t.Fatalf("EstimateMessageTokens() = %d, content text appears double-counted", tokens)
	}
}

func TestCoreSendsAssistantProfileToTriggeredRun(t *testing.T) {
	t.Parallel()

	requests := make(chan providers.Request, 1)
	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		requests <- request
		return providers.Response{Text: "profile received"}, nil
	}

	app := newTestCore(t, provider)
	app.SetAssistantProfile(core.AssistantProfile{
		Name:               "Mia",
		SystemPrompt:       "Technical system prompt.",
		CustomInstructions: "Friendly assistant.",
	})
	ctx := context.Background()
	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Docs"})
	accepted, err := app.AcceptTriggeredRun(ctx, core.HandleTriggeredRunInput{
		TriggerID: "fire_test",
		SessionID: session.ID,
		Text:      "scheduled task",
	})
	if err != nil {
		t.Fatalf("AcceptTriggeredRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	select {
	case request := <-requests:
		if !strings.Contains(request.SystemPrompt, `configured assistant name is "Mia"`) {
			t.Fatalf("SystemPrompt = %q, want assistant name", request.SystemPrompt)
		}
		if !strings.Contains(request.SystemPrompt, "Technical system prompt.") {
			t.Fatalf("SystemPrompt = %q, want technical prompt", request.SystemPrompt)
		}
		if request.CustomInstructions != "Friendly assistant." {
			t.Fatalf("CustomInstructions = %q, want custom instructions", request.CustomInstructions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider request was not captured")
	}
}

func TestCoreAcceptRunRequiresExplicitOrExistingBinding(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil)
	ctx := context.Background()

	if _, err := app.CreateSession(ctx, core.CreateSessionInput{Title: "Docs"}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	_, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "hello",
	})
	if !errors.Is(err, core.ErrSessionSelectionRequired) {
		t.Fatalf("AcceptRun() error = %v, want ErrSessionSelectionRequired", err)
	}
}

func TestCoreRenameAndDeleteSession(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil)
	ctx := context.Background()

	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Docs"})

	renamed, err := app.RenameSession(ctx, core.RenameSessionInput{
		SessionID: session.ID,
		Title:     "Renamed Docs",
	})
	if err != nil {
		t.Fatalf("RenameSession() error = %v", err)
	}
	if renamed.Title != "Renamed Docs" {
		t.Fatalf("RenameSession().Title = %q, want %q", renamed.Title, "Renamed Docs")
	}

	sessions, err := app.ListSessions(ctx, core.SessionListFilter{})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].Title != "Renamed Docs" {
		t.Fatalf("ListSessions() = %#v, want renamed session", sessions)
	}

	if err := app.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := app.RenameSession(ctx, core.RenameSessionInput{
		SessionID: session.ID,
		Title:     "Again",
	}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("RenameSession() after delete error = %v, want ErrNotFound", err)
	}
}

func TestCoreSessionWorkingDirPersistsAndUpdates(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil)
	ctx := context.Background()

	session := createSession(t, app, ctx, core.CreateSessionInput{
		Title:      "Docs",
		WorkingDir: "/workspace/original",
	})

	sessions, err := app.ListSessions(ctx, core.SessionListFilter{})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(ListSessions()) = %d, want 1", len(sessions))
	}
	if sessions[0].ID != session.ID {
		t.Fatalf("ListSessions()[0].ID = %q, want %q", sessions[0].ID, session.ID)
	}
	if sessions[0].WorkingDir != "/workspace/original" {
		t.Fatalf("ListSessions()[0].WorkingDir = %q, want %q", sessions[0].WorkingDir, "/workspace/original")
	}
	bindSession(t, app, ctx, session.ID)

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "hello",
		WorkingDir:  "/workspace/matrixclaw",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	sessions, err = app.ListSessions(ctx, core.SessionListFilter{})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if sessions[0].WorkingDir != "/workspace/matrixclaw" {
		t.Fatalf("ListSessions()[0].WorkingDir = %q, want %q", sessions[0].WorkingDir, "/workspace/matrixclaw")
	}
}

func TestCoreExecuteToolUsesSessionWorkingDirFallback(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	workdir := t.TempDir()
	target := filepath.Join(workdir, "notes.txt")
	if err := os.WriteFile(target, []byte("hello from session cwd"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	session := createSession(t, app, ctx, core.CreateSessionInput{
		Title:      "Tools",
		WorkingDir: workdir,
	})

	result, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID: session.ID,
		ToolName:  "read",
		Args:      json.RawMessage(`{"file_path":"notes.txt"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if result.ToolResultMessage == nil {
		t.Fatal("ExecuteTool() should return tool result message")
	}
	if !strings.Contains(result.ToolResultMessage.Content, "hello from session cwd") {
		t.Fatalf("tool result content = %q, want file contents", result.ToolResultMessage.Content)
	}
}

func TestCoreExecuteRunMarksRunFailedWhenProviderFails(t *testing.T) {
	t.Parallel()

	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		return providers.Response{}, errors.New("boom")
	}

	app := newTestCore(t, provider)
	ctx := context.Background()

	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Docs"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "hello",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}

	run := waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusFailed)
	if run.Status != core.RunStatusFailed {
		t.Fatalf("GetRun().Status = %q, want %q", run.Status, core.RunStatusFailed)
	}

	messages, listErr := app.ListMessages(ctx, session.ID, 10)
	if listErr != nil {
		t.Fatalf("ListMessages() error = %v", listErr)
	}
	if len(messages) != 2 {
		t.Fatalf("len(ListMessages()) = %d, want 2", len(messages))
	}
	if messages[1].Role != core.MessageRoleAssistant {
		t.Fatalf("messages[1].Role = %q, want assistant", messages[1].Role)
	}
	if len(messages[1].Parts) == 0 || messages[1].Parts[len(messages[1].Parts)-1].Finish == nil {
		t.Fatalf("assistant failure message missing finish part: %#v", messages[1].Parts)
	}
	if messages[1].Parts[len(messages[1].Parts)-1].Finish.Reason != "error" {
		t.Fatalf("assistant finish reason = %q, want error", messages[1].Parts[len(messages[1].Parts)-1].Finish.Reason)
	}
	if !strings.Contains(messages[1].Parts[len(messages[1].Parts)-1].Finish.Message, "boom") {
		t.Fatalf("assistant finish message = %q, want boom", messages[1].Parts[len(messages[1].Parts)-1].Finish.Message)
	}
}

func TestCoreExecuteRunStreamsAssistantUpdates(t *testing.T) {
	t.Parallel()

	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		if err := providers.StreamText(ctx, "hello "); err != nil {
			return providers.Response{}, err
		}
		if err := providers.StreamText(ctx, "world"); err != nil {
			return providers.Response{}, err
		}
		return providers.Response{
			Text:     "hello world",
			Model:    "stub-model",
			Provider: "stub-provider",
		}, nil
	}

	app := newTestCore(t, provider)
	ctx := context.Background()

	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Docs"})

	events := app.SubscribeEvents(ctx, session.ID)
	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "hello",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}

	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	var (
		assistantCreated bool
		assistantUpdated bool
		sawPartial       bool
		runUpdated       bool
	)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (!assistantCreated || !assistantUpdated || !sawPartial || !runUpdated) {
		select {
		case event := <-events:
			if event.Type == core.EventRunUpdated {
				runUpdated = true
				continue
			}
			message, ok := event.Payload.(core.Message)
			if !ok || message.Role != core.MessageRoleAssistant {
				continue
			}
			switch event.Type {
			case core.EventMessageCreated:
				assistantCreated = true
				if strings.TrimSpace(message.Content) != "" && message.Content != "hello world" {
					sawPartial = true
				}
			case core.EventMessageUpdated:
				assistantUpdated = true
				if strings.TrimSpace(message.Content) != "" && message.Content != "hello world" {
					sawPartial = true
				}
			}
		case <-time.After(10 * time.Millisecond):
		}
	}

	if !assistantCreated {
		t.Fatal("did not observe assistant message.created event")
	}
	if !assistantUpdated {
		t.Fatal("did not observe assistant message.updated event")
	}
	if !sawPartial {
		t.Fatal("did not observe partial assistant content before completion")
	}
	if !runUpdated {
		t.Fatal("did not observe run.updated event")
	}

	messages, err := app.ListMessages(ctx, session.ID, 10)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(ListMessages()) = %d, want 2", len(messages))
	}
	if messages[1].Content != "hello world" {
		t.Fatalf("assistant message content = %q, want %q", messages[1].Content, "hello world")
	}
	if messages[1].Model != "stub-model" {
		t.Fatalf("assistant model = %q, want %q", messages[1].Model, "stub-model")
	}
	if messages[1].Provider != "stub-provider" {
		t.Fatalf("assistant provider = %q, want %q", messages[1].Provider, "stub-provider")
	}
}

func TestCoreToolApprovalAndExecutionFlow(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	workdir := t.TempDir()

	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Tools"})

	events := app.SubscribeEvents(ctx, session.ID)
	writeArgs, _ := json.Marshal(tools.WriteParams{
		FilePath: "notes.txt",
		Content:  "hello\nworld\n",
	})

	pending, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		ToolName:   "write",
		WorkingDir: workdir,
		Args:       writeArgs,
	})
	if err != nil {
		t.Fatalf("ExecuteTool() pending error = %v", err)
	}
	if pending.Approval == nil {
		t.Fatal("ExecuteTool() should return approval")
	}

	approvals, err := app.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals() error = %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("len(ListApprovals()) = %d, want 1", len(approvals))
	}

	resolved, err := app.ResolveApproval(ctx, pending.Approval.ID, true)
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if resolved.State != core.ApprovalStateApproved {
		t.Fatalf("ResolveApproval().State = %q, want %q", resolved.State, core.ApprovalStateApproved)
	}

	messages, err := app.ListMessages(ctx, session.ID, 20)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(ListMessages()) = %d, want 2", len(messages))
	}
	if messages[0].Parts[0].ToolCall == nil || !messages[0].Parts[0].ToolCall.Finished {
		t.Fatalf("tool call message not finished: %#v", messages[0].Parts)
	}
	if messages[1].Parts[0].ToolResult == nil {
		t.Fatalf("tool result message missing tool result part: %#v", messages[1].Parts)
	}

	foundApprovalRequest := false
	foundApprovalResult := false
	foundFileVersion := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (!foundApprovalRequest || !foundApprovalResult || !foundFileVersion) {
		select {
		case event := <-events:
			switch event.Type {
			case core.EventApprovalRequest:
				foundApprovalRequest = true
			case core.EventApprovalResult:
				foundApprovalResult = true
			case core.EventFileVersioned:
				foundFileVersion = true
			}
		case <-time.After(10 * time.Millisecond):
		}
	}

	if !foundApprovalRequest {
		t.Fatal("did not observe approval.requested event")
	}
	if !foundApprovalResult {
		t.Fatal("did not observe approval.resolved event")
	}
	if !foundFileVersion {
		t.Fatal("did not observe file.versioned event")
	}
}

func TestCorePlanToolsUpdateSessionPlanWithoutApproval(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil)
	app.WithTools(tools.NewCoreCodingRegistry(core.PlanToolExecutors(app)...))
	ctx := context.Background()
	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Plan tools"})

	goalArgs, _ := json.Marshal(map[string]string{"goal": "Ship agent planning"})
	goalResult, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID: session.ID,
		ToolName:  "plan_set_goal",
		Args:      goalArgs,
	})
	if err != nil {
		t.Fatalf("ExecuteTool(plan_set_goal) error = %v", err)
	}
	if goalResult.Approval != nil {
		t.Fatal("plan_set_goal requested approval, want auto execution")
	}

	addArgs, _ := json.Marshal(map[string]string{"text": "Add plan tools"})
	if _, err := app.ExecuteTool(ctx, core.ExecuteToolInput{SessionID: session.ID, ToolName: "plan_add_item", Args: addArgs}); err != nil {
		t.Fatalf("ExecuteTool(plan_add_item) error = %v", err)
	}

	plan, err := app.SessionPlan(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionPlan() error = %v", err)
	}
	if plan.Goal != "Ship agent planning" || len(plan.Items) != 1 || plan.Items[0].Text != "Add plan tools" {
		t.Fatalf("plan = %#v, want goal and one item", plan)
	}

	childArgs, _ := json.Marshal(map[string]string{"text": "Render plan tree", "parent_id": plan.Items[0].ID})
	if _, err := app.ExecuteTool(ctx, core.ExecuteToolInput{SessionID: session.ID, ToolName: "plan_add_item", Args: childArgs}); err != nil {
		t.Fatalf("ExecuteTool(plan_add_item child) error = %v", err)
	}
	plan, err = app.SessionPlan(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionPlan() after child error = %v", err)
	}
	if len(plan.Items) != 2 || plan.Items[1].ParentID != plan.Items[0].ID {
		t.Fatalf("plan child = %#v, want child under first item", plan.Items)
	}

	updateArgs, _ := json.Marshal(map[string]string{"item_id": plan.Items[0].ID, "status": "done"})
	if _, err := app.ExecuteTool(ctx, core.ExecuteToolInput{SessionID: session.ID, ToolName: "plan_update_item", Args: updateArgs}); err != nil {
		t.Fatalf("ExecuteTool(plan_update_item) error = %v", err)
	}
	plan, err = app.SessionPlan(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionPlan() after update error = %v", err)
	}
	if plan.Items[0].Status != core.PlanItemDone {
		t.Fatalf("plan item status = %q, want done", plan.Items[0].Status)
	}
}

func TestCoreProviderRequestIncludesPlanToolsAndPrompt(t *testing.T) {
	t.Parallel()

	requests := make(chan providers.Request, 1)
	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		requests <- request
		return providers.Response{Text: "planned"}, nil
	}

	app := newTestCore(t, provider)
	app.WithTools(tools.NewCoreCodingRegistry(core.PlanToolExecutors(app)...))
	ctx := context.Background()
	createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Plan request"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "make a plan and do the task",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	select {
	case request := <-requests:
		if !strings.Contains(request.SystemPrompt, "Use plan tools for multi-step work") {
			t.Fatalf("SystemPrompt = %q, want plan tool instruction", request.SystemPrompt)
		}
		toolNames := map[string]bool{}
		for _, tool := range request.Tools {
			toolNames[tool.Name] = true
		}
		for _, name := range []string{"plan_get", "plan_set_goal", "plan_add_item", "plan_update_item", "plan_clear"} {
			if !toolNames[name] {
				t.Fatalf("provider request missing tool %q; tools=%#v", name, request.Tools)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider request was not captured")
	}
}

func TestCoreCompletesActivePlanItemsWhenRunFinishesWithoutPendingWork(t *testing.T) {
	t.Parallel()

	provider := newProviderStub()
	provider.GenerateFunc = func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "All planned work is complete."}, nil
	}
	app := newTestCore(t, provider)
	ctx := context.Background()
	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Plan completion"})

	plan, err := app.SetSessionGoal(ctx, session.ID, "finish plan")
	if err != nil {
		t.Fatalf("SetSessionGoal() error = %v", err)
	}
	plan, err = app.AddPlanItem(ctx, session.ID, "write summary", "")
	if err != nil {
		t.Fatalf("AddPlanItem() error = %v", err)
	}
	plan, err = app.UpdatePlanItem(ctx, session.ID, plan.Items[0].ID, core.PlanItemActive, "")
	if err != nil {
		t.Fatalf("UpdatePlanItem(active) error = %v", err)
	}

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "continue",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	plan, err = app.SessionPlan(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionPlan() error = %v", err)
	}
	if plan.Items[0].Status != core.PlanItemDone {
		t.Fatalf("plan item status = %q, want done", plan.Items[0].Status)
	}
}

func TestCoreCompletesPlanRunPromptItemAndParentWhenRunFinishes(t *testing.T) {
	t.Parallel()

	provider := newProviderStub()
	provider.GenerateFunc = func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "The requested plan item is complete."}, nil
	}
	app := newTestCore(t, provider)
	ctx := context.Background()
	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Plan item runner"})

	plan, err := app.SetSessionGoal(ctx, session.ID, "finish plan")
	if err != nil {
		t.Fatalf("SetSessionGoal() error = %v", err)
	}
	plan, err = app.AddPlanItem(ctx, session.ID, "parent", "")
	if err != nil {
		t.Fatalf("AddPlanItem(parent) error = %v", err)
	}
	parentID := plan.Items[0].ID
	plan, err = app.AddPlanItem(ctx, session.ID, "child", parentID)
	if err != nil {
		t.Fatalf("AddPlanItem(child) error = %v", err)
	}
	childID := plan.Items[1].ID
	if _, _, err := app.StartSessionPlanRun(ctx, session.ID, false); err != nil {
		t.Fatalf("StartSessionPlanRun() error = %v", err)
	}

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "Execute the next session plan item.\n\nPlan item id: " + childID + "\nPlan item text: child",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	plan, err = app.SessionPlan(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionPlan() error = %v", err)
	}
	if plan.Items[0].Status != core.PlanItemDone || plan.Items[1].Status != core.PlanItemDone {
		t.Fatalf("plan item statuses = (%q,%q), want done/done", plan.Items[0].Status, plan.Items[1].Status)
	}
}

func TestCoreKeepsPlanRunPromptItemOpenWhenAssistantReportsBlocked(t *testing.T) {
	t.Parallel()

	provider := newProviderStub()
	provider.GenerateFunc = func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "PLAN_BLOCKED: missing credentials."}, nil
	}
	app := newTestCore(t, provider)
	ctx := context.Background()
	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Blocked plan item"})

	plan, err := app.SetSessionGoal(ctx, session.ID, "finish plan")
	if err != nil {
		t.Fatalf("SetSessionGoal() error = %v", err)
	}
	plan, err = app.AddPlanItem(ctx, session.ID, "needs credentials", "")
	if err != nil {
		t.Fatalf("AddPlanItem() error = %v", err)
	}
	itemID := plan.Items[0].ID
	if _, _, err := app.StartSessionPlanRun(ctx, session.ID, false); err != nil {
		t.Fatalf("StartSessionPlanRun() error = %v", err)
	}

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "Execute the next session plan item.\n\nPlan item id: " + itemID + "\nPlan item text: needs credentials",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	plan, err = app.SessionPlan(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionPlan() error = %v", err)
	}
	if plan.Items[0].Status != core.PlanItemActive {
		t.Fatalf("plan item status = %q, want active", plan.Items[0].Status)
	}
}

func TestCoreDoesNotCompleteActivePlanItemsWhenPendingWorkRemains(t *testing.T) {
	t.Parallel()

	provider := newProviderStub()
	provider.GenerateFunc = func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "Partial work is complete."}, nil
	}
	app := newTestCore(t, provider)
	ctx := context.Background()
	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Partial plan"})

	plan, err := app.SetSessionGoal(ctx, session.ID, "finish plan")
	if err != nil {
		t.Fatalf("SetSessionGoal() error = %v", err)
	}
	plan, err = app.AddPlanItem(ctx, session.ID, "active task", "")
	if err != nil {
		t.Fatalf("AddPlanItem(active) error = %v", err)
	}
	plan, err = app.AddPlanItem(ctx, session.ID, "pending task", "")
	if err != nil {
		t.Fatalf("AddPlanItem(pending) error = %v", err)
	}
	plan, err = app.UpdatePlanItem(ctx, session.ID, plan.Items[0].ID, core.PlanItemActive, "")
	if err != nil {
		t.Fatalf("UpdatePlanItem(active) error = %v", err)
	}

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "continue",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	plan, err = app.SessionPlan(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionPlan() error = %v", err)
	}
	if plan.Items[0].Status != core.PlanItemActive {
		t.Fatalf("active item status = %q, want active", plan.Items[0].Status)
	}
	if plan.Items[1].Status != core.PlanItemPending {
		t.Fatalf("pending item status = %q, want pending", plan.Items[1].Status)
	}
}

func TestCoreResolveApprovalRejectsConflictingSecondDecision(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	workdir := t.TempDir()

	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Approval state"})

	writeArgs, _ := json.Marshal(tools.WriteParams{
		FilePath: "notes.txt",
		Content:  "hello\nworld\n",
	})

	pending, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		ToolName:   "write",
		WorkingDir: workdir,
		Args:       writeArgs,
	})
	if err != nil {
		t.Fatalf("ExecuteTool() pending error = %v", err)
	}
	if pending.Approval == nil {
		t.Fatal("ExecuteTool() should return approval")
	}

	if _, err := app.ResolveApproval(ctx, pending.Approval.ID, true); err != nil {
		t.Fatalf("ResolveApproval(approve) error = %v", err)
	}
	if _, err := app.ResolveApproval(ctx, pending.Approval.ID, false); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("ResolveApproval(conflicting second decision) error = %v, want ErrInvalidInput", err)
	}
}

func TestCoreExecuteToolGeneratesUniqueApprovalsForRepeatedEdits(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	workdir := t.TempDir()

	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Repeated approvals"})

	writeArgs, _ := json.Marshal(tools.WriteParams{
		FilePath: "notes.txt",
		Content:  "one\n",
	})
	first, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		ToolName:   "write",
		WorkingDir: workdir,
		Args:       writeArgs,
	})
	if err != nil {
		t.Fatalf("ExecuteTool(first pending) error = %v", err)
	}
	if first.Approval == nil {
		t.Fatal("first ExecuteTool() should return approval")
	}
	if _, err := app.ResolveApproval(ctx, first.Approval.ID, true); err != nil {
		t.Fatalf("ResolveApproval(first) error = %v", err)
	}

	second, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		ToolName:   "write",
		WorkingDir: workdir,
		Args:       writeArgs,
	})
	if err != nil {
		t.Fatalf("ExecuteTool(second pending) error = %v", err)
	}
	if second.Approval == nil {
		t.Fatal("second ExecuteTool() should return approval")
	}
	if second.Approval.ID == first.Approval.ID {
		t.Fatalf("second approval ID = %q, want unique ID different from %q", second.Approval.ID, first.Approval.ID)
	}

	approvals, err := app.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals(pending) error = %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("len(ListApprovals(pending)) = %d, want 1", len(approvals))
	}
	if approvals[0].ID != second.Approval.ID {
		t.Fatalf("pending approval ID = %q, want %q", approvals[0].ID, second.Approval.ID)
	}
}

func TestCorePermissionModesForToolExecution(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		mode         core.PermissionMode
		toolName     string
		args         any
		wantApproval bool
	}{
		{
			name:         "accept edits auto-approves file mutation in working dir",
			mode:         core.PermissionModeAcceptEdits,
			toolName:     "write",
			args:         tools.WriteParams{FilePath: "notes.txt", Content: "one\n"},
			wantApproval: false,
		},
		{
			name:         "accept edits still requires bash approval",
			mode:         core.PermissionModeAcceptEdits,
			toolName:     "bash",
			args:         tools.BashParams{Command: "printf hello"},
			wantApproval: true,
		},
		{
			name:         "full auto auto-approves bash",
			mode:         core.PermissionModeFullAuto,
			toolName:     "bash",
			args:         tools.BashParams{Command: "printf hello"},
			wantApproval: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			app := newTestCore(t, nil).WithTools(newCoreCodingRegistry())
			ctx := context.Background()
			workdir := t.TempDir()

			session := createSession(t, app, ctx, core.CreateSessionInput{
				Title:          tc.name,
				WorkingDir:     workdir,
				PermissionMode: tc.mode,
			})

			args, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatalf("Marshal(args) error = %v", err)
			}
			result, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
				SessionID:  session.ID,
				WorkingDir: workdir,
				ToolName:   tc.toolName,
				Args:       args,
			})
			if err != nil {
				t.Fatalf("ExecuteTool(%s) error = %v", tc.toolName, err)
			}
			if gotApproval := result.Approval != nil; gotApproval != tc.wantApproval {
				t.Fatalf("ExecuteTool(%s) approval present = %v, want %v", tc.toolName, gotApproval, tc.wantApproval)
			}
			if !tc.wantApproval && result.ToolResultMessage == nil {
				t.Fatalf("ExecuteTool(%s) should create tool result when auto-approved", tc.toolName)
			}
		})
	}
}

func TestCoreFullAutoAllowsMutationOutsideWorkingDir(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	root := t.TempDir()
	workdir := filepath.Join(root, "work")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workdir) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}
	outsideFile := filepath.Join(outside, "notes.txt")

	session := createSession(t, app, ctx, core.CreateSessionInput{
		Title:          "full auto outside mutation",
		WorkingDir:     workdir,
		PermissionMode: core.PermissionModeFullAuto,
	})
	args, _ := json.Marshal(tools.WriteParams{FilePath: outsideFile, Content: "full auto\n"})

	result, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		WorkingDir: workdir,
		ToolName:   "write",
		Args:       args,
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if result.Approval != nil {
		t.Fatalf("Approval = %#v, want full-auto write without approval", result.Approval)
	}
	if result.ToolResultMessage == nil || result.ToolResultMessage.Parts[0].ToolResult == nil {
		t.Fatalf("ToolResultMessage = %#v, want completed write", result.ToolResultMessage)
	}
	if content, err := os.ReadFile(outsideFile); err != nil || string(content) != "full auto\n" {
		t.Fatalf("outside file content = %q err=%v, want full auto write", string(content), err)
	}
}

func TestCoreAcceptEditsRequiresApprovalForSymlinkEscape(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	root := t.TempDir()
	workdir := filepath.Join(root, "work")
	outside := filepath.Join(root, "outside")
	outsideSubdir := filepath.Join(outside, "sub")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workdir) error = %v", err)
	}
	if err := os.MkdirAll(outsideSubdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(outsideSubdir) error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(workdir, "link")); err != nil {
		t.Skipf("Symlink() unavailable: %v", err)
	}
	outsideFile := filepath.Join(outsideSubdir, "notes.txt")

	session := createSession(t, app, ctx, core.CreateSessionInput{
		Title:          "accept edits symlink escape",
		WorkingDir:     workdir,
		PermissionMode: core.PermissionModeAcceptEdits,
	})
	args, _ := json.Marshal(tools.WriteParams{FilePath: "link/sub/notes.txt", Content: "outside\n"})

	result, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		WorkingDir: workdir,
		ToolName:   "write",
		Args:       args,
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if result.Approval == nil {
		t.Fatalf("Approval = nil, want approval for symlink escape")
	}
	if _, err := os.Stat(outsideFile); !os.IsNotExist(err) {
		t.Fatalf("outside file was written without approval, stat err = %v", err)
	}
}

func TestCoreApprovalReplayUsesParentDirForExactMutationPath(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	workdir := t.TempDir()
	target := filepath.Join(workdir, "notes.txt")
	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "exact approval path"})
	args, _ := json.Marshal(tools.WriteParams{FilePath: "notes.txt", Content: "approved\n"})

	pending, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		WorkingDir: workdir,
		ToolName:   "write",
		Args:       args,
	})
	if err != nil {
		t.Fatalf("ExecuteTool(pending) error = %v", err)
	}
	if pending.Approval == nil {
		t.Fatal("ExecuteTool() approval = nil, want pending approval")
	}
	if pending.Approval.Path != target {
		t.Fatalf("approval path = %q, want exact target %q", pending.Approval.Path, target)
	}
	if _, err := app.ResolveApproval(ctx, pending.Approval.ID, true); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "approved\n" {
		t.Fatalf("target content = %q err=%v, want approved write", string(content), err)
	}
}

func TestCoreFileVersionEventsIncrementPerPath(t *testing.T) {
	t.Parallel()

	app := newTestCore(t, nil).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	workdir := t.TempDir()

	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Files"})

	writeArgs, _ := json.Marshal(tools.WriteParams{
		FilePath: "notes.txt",
		Content:  "one\n",
	})
	first, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		ToolName:   "write",
		WorkingDir: workdir,
		Args:       writeArgs,
	})
	if err != nil {
		t.Fatalf("ExecuteTool(first pending) error = %v", err)
	}
	if _, err := app.ResolveApproval(ctx, first.Approval.ID, true); err != nil {
		t.Fatalf("ResolveApproval(first) error = %v", err)
	}

	editArgs, _ := json.Marshal(tools.EditParams{
		FilePath:  "notes.txt",
		OldString: "one",
		NewString: "two",
	})
	second, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		ToolName:   "edit",
		WorkingDir: workdir,
		Args:       editArgs,
	})
	if err != nil {
		t.Fatalf("ExecuteTool(second pending) error = %v", err)
	}
	events := app.SubscribeEvents(ctx, session.ID)
	if _, err := app.ResolveApproval(ctx, second.Approval.ID, true); err != nil {
		t.Fatalf("ResolveApproval(second) error = %v", err)
	}

	found := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !found {
		select {
		case event := <-events:
			if event.Type != core.EventFileVersioned {
				continue
			}
			file, ok := event.Payload.(core.FileSnapshot)
			if !ok {
				t.Fatalf("file.versioned payload type = %T, want core.FileSnapshot", event.Payload)
			}
			if file.Version != 1 {
				t.Fatalf("file.Version = %d, want 1 on second change", file.Version)
			}
			found = true
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !found {
		t.Fatal("did not observe second file.versioned event")
	}
}

func TestCoreExecuteRunAutoToolLoopCompletes(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	filePath := filepath.Join(workdir, "note.txt")
	if err := os.WriteFile(filePath, []byte("hello tool loop\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	provider := newProviderStub()
	callCount := 0
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		callCount++
		switch callCount {
		case 1:
			if len(request.Tools) == 0 {
				t.Fatal("provider request missing tools")
			}
			args, _ := json.Marshal(tools.ReadParams{FilePath: filePath})
			return providers.Response{
				ToolCalls: []providers.ToolCall{{
					ID:        "tool_call_1",
					Name:      "read",
					Arguments: args,
				}},
				Model:    "stub-model",
				Provider: "stub-provider",
			}, nil
		case 2:
			foundToolResult := false
			for _, message := range request.Messages {
				if message.Role == "tool" && strings.Contains(message.Content, "hello tool loop") {
					foundToolResult = true
					break
				}
			}
			if !foundToolResult {
				t.Fatalf("second provider request missing tool result: %#v", request.Messages)
			}
			return providers.Response{
				Text:     "done after tool",
				Model:    "stub-model",
				Provider: "stub-provider",
			}, nil
		default:
			t.Fatalf("unexpected provider call %d", callCount)
			return providers.Response{}, nil
		}
	}

	app := newTestCore(t, provider).WithTools(newCoreCodingRegistry())
	ctx := context.Background()

	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Tools"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "inspect the file",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}

	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	messages, err := app.ListMessages(ctx, session.ID, 20)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("len(ListMessages()) = %d, want 4", len(messages))
	}
	if messages[1].Parts[0].ToolCall == nil || messages[1].Parts[0].ToolCall.Name != "read" {
		t.Fatalf("messages[1] should be tool call: %#v", messages[1].Parts)
	}
	if messages[2].Role != core.MessageRoleTool {
		t.Fatalf("messages[2].Role = %q, want tool", messages[2].Role)
	}
	if messages[3].Content != "done after tool" {
		t.Fatalf("final assistant content = %q, want %q", messages[3].Content, "done after tool")
	}
}

func TestCoreOmitsToolUseForDisabledRuntimeProfile(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	filePath := filepath.Join(workdir, "note.txt")
	if err := os.WriteFile(filePath, []byte("history from tool\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	requests := make(chan providers.Request, 1)
	provider := &profiledRuntime{
		providerStub: newProviderStub(),
		profile: providers.RuntimeProfile{
			ToolUseMode: providers.ToolUseDisabled,
		},
	}
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		requests <- request
		return providers.Response{
			Text:     "text-only response",
			Model:    "stub-model",
			Provider: "stub-provider",
		}, nil
	}

	app := newTestCore(t, provider).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	session := createBoundSession(t, app, ctx, core.CreateSessionInput{
		Title:      "Text only",
		WorkingDir: workdir,
	})

	args, _ := json.Marshal(tools.ReadParams{FilePath: filePath})
	if _, err := app.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  session.ID,
		ToolName:   "read",
		ToolCallID: "tool_call_read",
		WorkingDir: workdir,
		Approved:   true,
		Args:       args,
	}); err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "continue with text only",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	select {
	case request := <-requests:
		if len(request.Tools) != 0 {
			t.Fatalf("provider request Tools len = %d, want 0", len(request.Tools))
		}
		foundToolResultText := false
		for _, message := range request.Messages {
			if len(message.ToolCalls) != 0 {
				t.Fatalf("provider request contains tool calls: %#v", request.Messages)
			}
			if message.Role == "tool" || strings.TrimSpace(message.ToolCallID) != "" {
				t.Fatalf("provider request contains tool result history: %#v", request.Messages)
			}
			if strings.Contains(message.Content, "Previous tool result from read:") && strings.Contains(message.Content, "history from tool") {
				foundToolResultText = true
			}
		}
		if !foundToolResultText {
			t.Fatalf("provider request missing text-only tool result context: %#v", request.Messages)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider request was not captured")
	}
}

func TestCoreKeepsToolsForNativeRuntimeProfile(t *testing.T) {
	t.Parallel()

	requests := make(chan providers.Request, 1)
	provider := &profiledRuntime{
		providerStub: newProviderStub(),
		profile: providers.RuntimeProfile{
			ToolUseMode: providers.ToolUseNative,
		},
	}
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		requests <- request
		return providers.Response{
			Text:     "native tools available",
			Model:    "stub-model",
			Provider: "stub-provider",
		}, nil
	}

	app := newTestCore(t, provider).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Native tools"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "list files",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	select {
	case request := <-requests:
		if len(request.Tools) == 0 {
			t.Fatal("provider request Tools len = 0, want tools for native runtime")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider request was not captured")
	}
}

func TestCoreExecuteRunApprovalResumeCompletesRun(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	filePath := filepath.Join(workdir, "draft.txt")

	provider := newProviderStub()
	callCount := 0
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		callCount++
		switch callCount {
		case 1:
			args, _ := json.Marshal(tools.WriteParams{
				FilePath: filePath,
				Content:  "saved via tool\n",
			})
			return providers.Response{
				ToolCalls: []providers.ToolCall{{
					ID:        "tool_call_write",
					Name:      "write",
					Arguments: args,
				}},
				Model:    "stub-model",
				Provider: "stub-provider",
			}, nil
		case 2:
			foundToolResult := false
			for _, message := range request.Messages {
				if message.Role == "tool" && strings.Contains(message.Content, "File written") {
					foundToolResult = true
					break
				}
			}
			if !foundToolResult {
				t.Fatalf("second provider request missing written tool result: %#v", request.Messages)
			}
			return providers.Response{
				Text:     "write complete",
				Model:    "stub-model",
				Provider: "stub-provider",
			}, nil
		default:
			t.Fatalf("unexpected provider call %d", callCount)
			return providers.Response{}, nil
		}
	}

	app := newTestCore(t, provider).WithTools(newCoreCodingRegistry())
	ctx := context.Background()

	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Approvals"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "write the file",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}

	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusWaitingApproval)

	approvals, err := app.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals() error = %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("len(ListApprovals()) = %d, want 1", len(approvals))
	}

	if _, err := app.ResolveApproval(ctx, approvals[0].ID, true); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "saved via tool\n" {
		t.Fatalf("file content = %q, want saved content", string(content))
	}

	messages, err := app.ListMessages(ctx, session.ID, 20)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("len(ListMessages()) = %d, want 4", len(messages))
	}
	if messages[3].Content != "write complete" {
		t.Fatalf("final assistant content = %q, want %q", messages[3].Content, "write complete")
	}
}

func TestCoreExecuteRunWaitsForAllApprovalsBeforeNextProviderTurn(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	fileA := filepath.Join(workdir, "a.txt")
	fileB := filepath.Join(workdir, "b.txt")

	provider := newProviderStub()
	var callCount int32
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		switch atomic.AddInt32(&callCount, 1) {
		case 1:
			argsA, _ := json.Marshal(tools.WriteParams{
				FilePath: fileA,
				Content:  "A\n",
			})
			argsB, _ := json.Marshal(tools.WriteParams{
				FilePath: fileB,
				Content:  "B\n",
			})
			return providers.Response{
				ToolCalls: []providers.ToolCall{
					{ID: "tool_call_write_a", Name: "write", Arguments: argsA},
					{ID: "tool_call_write_b", Name: "write", Arguments: argsB},
				},
				Model:    "stub-model",
				Provider: "stub-provider",
			}, nil
		case 2:
			foundA := false
			foundB := false
			for _, message := range request.Messages {
				if message.Role != "tool" {
					continue
				}
				if strings.Contains(message.Content, "a.txt") {
					foundA = true
				}
				if strings.Contains(message.Content, "b.txt") {
					foundB = true
				}
			}
			if !foundA || !foundB {
				t.Fatalf("final provider request missing tool results for both files: %#v", request.Messages)
			}
			return providers.Response{
				Text:     "both complete",
				Model:    "stub-model",
				Provider: "stub-provider",
			}, nil
		default:
			t.Fatalf("unexpected provider call %d", atomic.LoadInt32(&callCount))
			return providers.Response{}, nil
		}
	}

	app := newTestCore(t, provider).WithTools(newCoreCodingRegistry())
	ctx := context.Background()

	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Two approvals"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "write two files",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}

	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusWaitingApproval)

	approvals, err := app.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals() error = %v", err)
	}
	if len(approvals) != 2 {
		t.Fatalf("len(ListApprovals()) = %d, want 2", len(approvals))
	}

	if _, err := app.ResolveApproval(ctx, approvals[0].ID, true); err != nil {
		t.Fatalf("ResolveApproval(first) error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		remaining, err := app.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
		if err == nil && len(remaining) == 1 {
			_, errA := os.Stat(fileA)
			_, errB := os.Stat(fileB)
			if (errA == nil) != (errB == nil) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("provider call count after first approval = %d, want 1", got)
	}
	_, errA := os.Stat(fileA)
	_, errB := os.Stat(fileB)
	if (errA == nil) == (errB == nil) {
		t.Fatalf("after first approval expected exactly one file to exist, errA=%v errB=%v", errA, errB)
	}

	remaining, err := app.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals(remaining) error = %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("len(ListApprovals(remaining)) = %d, want 1", len(remaining))
	}

	if _, err := app.ResolveApproval(ctx, remaining[0].ID, true); err != nil {
		t.Fatalf("ResolveApproval(second) error = %v", err)
	}

	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	if got := atomic.LoadInt32(&callCount); got != 2 {
		t.Fatalf("provider call count after all approvals = %d, want 2", got)
	}
	if _, err := os.Stat(fileA); err != nil {
		t.Fatalf("fileA stat error = %v", err)
	}
	if _, err := os.Stat(fileB); err != nil {
		t.Fatalf("fileB stat error = %v", err)
	}
}

func TestCoreExecuteRunSanitizesAssistantReasoningOutput(t *testing.T) {
	t.Parallel()

	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		if err := providers.StreamText(ctx, "<thought>private"); err != nil {
			return providers.Response{}, err
		}
		if err := providers.StreamText(ctx, " reasoning</thought>\nFinal answer."); err != nil {
			return providers.Response{}, err
		}
		return providers.Response{
			Text:     "<thought>private reasoning</thought>\nFinal answer.",
			Model:    "stub-model",
			Provider: "stub-provider",
		}, nil
	}

	app := newTestCore(t, provider)
	ctx := context.Background()

	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Sanitize"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "answer",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	messages, err := app.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(ListMessages()) = %d, want 2", len(messages))
	}
	if messages[1].Content != "Final answer." {
		t.Fatalf("assistant content = %q, want sanitized final answer", messages[1].Content)
	}
	if strings.Contains(messages[1].Content, "thought") || strings.Contains(messages[1].Content, "private reasoning") {
		t.Fatalf("assistant content leaked reasoning: %q", messages[1].Content)
	}
}

func TestCoreExecuteRunStoresProviderReasoningContent(t *testing.T) {
	t.Parallel()

	reasoningContent := "private thinking"
	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		return providers.Response{
			Text:             "Final answer.",
			ReasoningContent: &reasoningContent,
			Model:            "stub-model",
			Provider:         "stub-provider",
		}, nil
	}

	app := newTestCore(t, provider)
	ctx := context.Background()

	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Reasoning"})
	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "answer",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	messages, err := app.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(ListMessages()) = %d, want 2", len(messages))
	}
	var gotReasoning string
	for _, part := range messages[1].Parts {
		if part.Reasoning != nil {
			gotReasoning = part.Reasoning.Text
			break
		}
	}
	if gotReasoning != reasoningContent {
		t.Fatalf("stored reasoning = %q, want %q", gotReasoning, reasoningContent)
	}
}

func TestCoreExecuteRunStoresEmptyProviderReasoningContentForToolCalls(t *testing.T) {
	t.Parallel()

	reasoningContent := ""
	args, _ := json.Marshal(tools.WriteParams{
		FilePath: filepath.Join(t.TempDir(), "notes.txt"),
		Content:  "hello",
	})
	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		return providers.Response{
			ReasoningContent: &reasoningContent,
			ToolCalls: []providers.ToolCall{{
				ID:        "call_1",
				Name:      "write",
				Arguments: args,
			}},
			Model:    "stub-model",
			Provider: "stub-provider",
		}, nil
	}

	app := newTestCore(t, provider).WithTools(newCoreCodingRegistry())
	ctx := context.Background()

	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Tool reasoning"})
	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{SessionID: session.ID, Text: "read"})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusWaitingApproval)

	messages, err := app.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	var found bool
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Reasoning == nil {
				continue
			}
			found = true
			if part.Reasoning.Text != "" {
				t.Fatalf("stored reasoning = %q, want empty string", part.Reasoning.Text)
			}
		}
	}
	if !found {
		t.Fatal("stored reasoning part not found")
	}
}

func TestCoreCompactSessionGeneratesSummaryWithoutTools(t *testing.T) {
	t.Parallel()

	requests := make(chan providers.Request, 2)
	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		requests <- request
		if strings.Contains(request.SystemPrompt, "compact matrixclaw chat histories") {
			return providers.Response{
				Text:     "Durable summary.",
				Model:    "stub-model",
				Provider: "stub-provider",
			}, nil
		}
		return providers.Response{
			Text:     "Initial assistant reply.",
			Model:    "stub-model",
			Provider: "stub-provider",
		}, nil
	}

	app := newTestCore(t, provider).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Summaries"})
	if _, err := app.SetSessionGoal(ctx, session.ID, "Ship context compaction"); err != nil {
		t.Fatalf("SetSessionGoal() error = %v", err)
	}
	if _, err := app.AddPlanItem(ctx, session.ID, "Keep tool context stable", ""); err != nil {
		t.Fatalf("AddPlanItem() error = %v", err)
	}
	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{SessionID: session.ID, Text: "Keep this context."})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)
	<-requests

	result, err := app.CompactSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("CompactSession() error = %v", err)
	}
	if !strings.Contains(result.Message.Content, "Durable summary.") {
		t.Fatalf("compact message content = %q, want summary", result.Message.Content)
	}

	compactRequest := <-requests
	if len(compactRequest.Tools) != 0 {
		t.Fatalf("compact provider request Tools len = %d, want 0", len(compactRequest.Tools))
	}
	if len(compactRequest.Messages) != 1 || !strings.Contains(compactRequest.Messages[0].Content, "Keep this context.") {
		t.Fatalf("compact provider messages = %#v, want session history prompt", compactRequest.Messages)
	}
	if !strings.Contains(compactRequest.Messages[0].Content, "Current session plan:") ||
		!strings.Contains(compactRequest.Messages[0].Content, "Ship context compaction") ||
		!strings.Contains(compactRequest.Messages[0].Content, "Keep tool context stable") {
		t.Fatalf("compact provider prompt missing current plan snapshot:\n%s", compactRequest.Messages[0].Content)
	}
}

func TestCoreCompactsAndRetriesOnContextLengthExceeded(t *testing.T) {
	t.Parallel()

	requests := make(chan providers.Request, 3)
	var normalCalls atomic.Int32
	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		requests <- request
		if strings.Contains(request.SystemPrompt, "compact matrixclaw chat histories") {
			return providers.Response{Text: "Durable retry summary.", Model: "stub-model", Provider: "stub-provider"}, nil
		}
		if normalCalls.Add(1) == 1 {
			return providers.Response{}, errors.New("context_length_exceeded: maximum context length exceeded")
		}
		return providers.Response{Text: "retried ok", Model: "stub-model", Provider: "stub-provider"}, nil
	}

	app := newTestCore(t, provider).WithTools(newCoreCodingRegistry())
	ctx := context.Background()
	session := createSession(t, app, ctx, core.CreateSessionInput{Title: "Retry"})
	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{SessionID: session.ID, Text: "Keep this context."})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	first := <-requests
	compact := <-requests
	retry := <-requests
	if strings.Contains(first.SystemPrompt, "compact matrixclaw chat histories") {
		t.Fatalf("first request unexpectedly compact request")
	}
	if !strings.Contains(compact.SystemPrompt, "compact matrixclaw chat histories") {
		t.Fatalf("second request SystemPrompt = %q, want compact request", compact.SystemPrompt)
	}
	if !strings.Contains(retry.SystemPrompt, "Session compact summary:") {
		t.Fatalf("retry SystemPrompt = %q, want compact summary", retry.SystemPrompt)
	}
}

func TestCoreCancelRunRejectsPendingApprovals(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	filePath := filepath.Join(workdir, "draft.txt")

	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		args, _ := json.Marshal(tools.WriteParams{
			FilePath: filePath,
			Content:  "saved via tool\n",
		})
		return providers.Response{
			ToolCalls: []providers.ToolCall{{
				ID:        "tool_call_write",
				Name:      "write",
				Arguments: args,
			}},
			Model:    "stub-model",
			Provider: "stub-provider",
		}, nil
	}

	app := newTestCore(t, provider).WithTools(newCoreCodingRegistry())
	ctx := context.Background()

	session := createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Cancel"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "write the file",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}

	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusWaitingApproval)

	run, err := app.CancelRun(ctx, accepted.Run.ID)
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if run.Status != core.RunStatusCanceled {
		t.Fatalf("CancelRun().Status = %q, want %q", run.Status, core.RunStatusCanceled)
	}

	pending, err := app.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals(pending) error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("len(ListApprovals(pending)) = %d, want 0", len(pending))
	}

	rejected, err := app.ListApprovals(ctx, session.ID, core.ApprovalStateRejected)
	if err != nil {
		t.Fatalf("ListApprovals(rejected) error = %v", err)
	}
	if len(rejected) != 1 {
		t.Fatalf("len(ListApprovals(rejected)) = %d, want 1", len(rejected))
	}
}

func TestCoreCancelRunCancelsActiveProviderRequest(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	canceled := make(chan struct{})
	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return providers.Response{}, ctx.Err()
	}

	app := newTestCore(t, provider)
	ctx := context.Background()
	createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Cancel"})

	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{
		Client:      "terminal",
		ExternalKey: "local",
		Text:        "wait",
	})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}

	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("provider request did not start")
	}

	if _, err := app.CancelRun(ctx, accepted.Run.ID); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}

	select {
	case <-canceled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("provider request was not canceled")
	}
}

func TestClientSnapshotIncludesRunTiming(t *testing.T) {
	t.Parallel()

	provider := newProviderStub()
	provider.GenerateFunc = func(ctx context.Context, request providers.Request) (providers.Response, error) {
		return providers.Response{Text: "done", Model: "stub", Provider: "stub"}, nil
	}

	app := newTestCore(t, provider)
	ctx := context.Background()
	createBoundSession(t, app, ctx, core.CreateSessionInput{Title: "Timing"})
	accepted, err := app.AcceptRun(ctx, core.HandleMessageInput{Client: "terminal", ExternalKey: "local", Text: "hello"})
	if err != nil {
		t.Fatalf("AcceptRun() error = %v", err)
	}
	waitForRunStatus(t, app, accepted.Run.ID, core.RunStatusCompleted)

	snapshot, err := app.ClientSnapshot(ctx, "terminal", "local")
	if err != nil {
		t.Fatalf("ClientSnapshot() error = %v", err)
	}
	if snapshot.Timing == nil {
		t.Fatal("ClientSnapshot().Timing = nil")
	}
	if snapshot.Timing.TotalMillis < 0 || snapshot.Timing.ModelMillis < 0 {
		t.Fatalf("ClientSnapshot().Timing = %#v", snapshot.Timing)
	}
}

func createSession(t *testing.T, app *core.Core, ctx context.Context, input core.CreateSessionInput) core.Session {
	t.Helper()

	session, err := app.CreateSession(ctx, input)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	return session
}

func bindSession(t *testing.T, app *core.Core, ctx context.Context, sessionID string) core.ClientBinding {
	t.Helper()

	binding, err := app.UseBinding(ctx, core.UseBindingInput{
		Client:      "terminal",
		ExternalKey: "local",
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("UseBinding() error = %v", err)
	}
	if binding.SessionID != sessionID {
		t.Fatalf("UseBinding().SessionID = %q, want %q", binding.SessionID, sessionID)
	}
	return binding
}

func createBoundSession(t *testing.T, app *core.Core, ctx context.Context, input core.CreateSessionInput) core.Session {
	t.Helper()

	session := createSession(t, app, ctx, input)
	bindSession(t, app, ctx, session.ID)
	return session
}

func newTestCore(t *testing.T, providerRuntime providers.Runtime) *core.Core {
	t.Helper()

	sqliteStore := openCoreTestStore(t)
	if providerRuntime == nil {
		providerRuntime = newProviderStub()
	}

	app := core.New(sqliteStore).WithSessionLLMs(newSessionLLMRegistryStub(providerRuntime))
	app.WithClock(func() time.Time { return time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC) })
	app.WithRunStarter(orchestration.NewStub(app))
	return app
}

func openCoreTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()

	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		if err := sqliteStore.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return sqliteStore
}

type profiledRuntime struct {
	*providerStub
	profile providers.RuntimeProfile
}

func (r *profiledRuntime) RuntimeProfile() providers.RuntimeProfile {
	return r.profile
}

type externalRuntimeStub struct {
	events        []externalagents.Event
	startSession  externalagents.ExternalSession
	resumeSession externalagents.ExternalSession
	startErr      error
	resumeErr     error
	resumeCalled  atomic.Bool
	startReq      atomic.Value
	resumeReq     atomic.Value
	inputText     atomic.Value
}

func (r *externalRuntimeStub) ID() string {
	return "codex-app"
}

func (r *externalRuntimeStub) DisplayName() string {
	return "Codex"
}

func (r *externalRuntimeStub) Aliases() []string {
	return []string{"codex"}
}

func (r *externalRuntimeStub) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true, Mode: "test"}
}

func (r *externalRuntimeStub) StartSession(_ context.Context, req externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	r.startReq.Store(req)
	if r.startErr != nil {
		return externalagents.ExternalSession{}, r.startErr
	}
	if r.startSession.ExternalThreadID == "" {
		return externalagents.ExternalSession{}, errors.New("unexpected StartSession")
	}
	return r.startSession, nil
}

func (r *externalRuntimeStub) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	r.resumeCalled.Store(true)
	r.resumeReq.Store(session)
	if r.resumeErr != nil {
		return externalagents.ExternalSession{}, r.resumeErr
	}
	if r.resumeSession.ExternalThreadID != "" {
		return r.resumeSession, nil
	}
	return session, nil
}

func (r *externalRuntimeStub) Send(ctx context.Context, session externalagents.ExternalSession, input externalagents.Input) (<-chan externalagents.Event, error) {
	r.inputText.Store(input.Text)
	out := make(chan externalagents.Event, len(r.events))
	go func() {
		defer close(out)
		for _, event := range r.events {
			event.AgentID = session.AgentID
			event.ExternalThreadID = session.ExternalThreadID
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (r *externalRuntimeStub) Interrupt(context.Context, externalagents.ExternalSession) error {
	return nil
}

func (r *externalRuntimeStub) Close() error {
	return nil
}

func waitForRunStatus(t *testing.T, app *core.Core, runID string, want core.RunStatus) core.Run {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := app.GetRun(context.Background(), runID)
		if err == nil && run.Status == want {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}

	run, err := app.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	t.Fatalf("GetRun().Status = %q, want %q", run.Status, want)
	return core.Run{}
}
