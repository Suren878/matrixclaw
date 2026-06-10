package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	. "github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestBusyQueuePersistsPendingInputWithoutStartingRun(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)

	session := saveSubagentTestSession(t, sqliteStore, "session_busy_queue", "Busy queue", t.TempDir())
	started, err := app.AcceptRun(ctx, HandleMessageInput{SessionID: session.ID, Text: "start"})
	if err != nil {
		t.Fatalf("AcceptRun start: %v", err)
	}
	if started.Status != AcceptRunStatusStarted {
		t.Fatalf("initial status = %q, want started", started.Status)
	}

	queued, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID: session.ID,
		Text:      "run this next",
		BusyMode:  BusyInputModeQueue,
	})
	if err != nil {
		t.Fatalf("AcceptRun queue: %v", err)
	}
	if queued.Status != AcceptRunStatusQueued {
		t.Fatalf("queued status = %q, want queued", queued.Status)
	}
	if len(starter.calls) != 1 {
		t.Fatalf("starter calls = %#v, want only initial run started", starter.calls)
	}
	pending, err := sqliteStore.ListPendingSessionInputs(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListPendingSessionInputs: %v", err)
	}
	if len(pending) != 1 || pending[0].Text != "run this next" || pending[0].Mode != BusyInputModeQueue {
		t.Fatalf("pending inputs = %#v, want queued input", pending)
	}
}

func TestAcceptRunCreatesSessionRunDelivery(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})

	session := saveSubagentTestSession(t, sqliteStore, "session_delivery", "Delivery", t.TempDir())
	result, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID:        session.ID,
		Client:           "telegram",
		ExternalKey:      "460252218",
		Text:             "hello",
		DeliveryAddress:  json.RawMessage(`{"chat_id":460252218,"thread_id":7}`),
		AllowAutoBindOne: true,
	})
	if err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}

	deliveries, err := app.ListClientDeliveries(ctx, ClientDeliveryFilter{
		Type:  ClientDeliveryTypeRun,
		RunID: result.Run.ID,
	})
	if err != nil {
		t.Fatalf("ListClientDeliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %#v, want one session run delivery", deliveries)
	}
	delivery := deliveries[0]
	if delivery.Client != "telegram" || delivery.ExternalKey != "460252218" || delivery.SessionID != session.ID || delivery.RunID != result.Run.ID {
		t.Fatalf("delivery identity = %#v", delivery)
	}
	if delivery.Status != ClientDeliveryStatusPending {
		t.Fatalf("delivery status = %q, want pending", delivery.Status)
	}
	if delivery.Summary != "hello" {
		t.Fatalf("delivery summary = %q, want user input summary", delivery.Summary)
	}
	if string(delivery.Address) != `{"chat_id":460252218,"thread_id":7}` {
		t.Fatalf("delivery address = %s", delivery.Address)
	}
}

func TestAcceptRunDoesNotCreateSessionRunDeliveryWithoutExternalKey(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})

	session := saveSubagentTestSession(t, sqliteStore, "session_delivery_without_key", "Delivery without key", t.TempDir())
	result, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID: session.ID,
		Client:    "telegram",
		Text:      "hello",
	})
	if err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}

	deliveries, err := app.ListClientDeliveries(ctx, ClientDeliveryFilter{
		Type:  ClientDeliveryTypeRun,
		RunID: result.Run.ID,
	})
	if err != nil {
		t.Fatalf("ListClientDeliveries: %v", err)
	}
	if len(deliveries) != 0 {
		t.Fatalf("deliveries = %#v, want none without external key", deliveries)
	}
}

func TestQueuedInputCreatesSessionRunDeliveryWhenConsumed(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	app.WithSessionLLMs(&subagentLLMs{runtime: &recordingRuntime{response: providers.Response{Text: "first done"}}})

	session := saveSubagentTestSession(t, sqliteStore, "session_queued_delivery", "Queued delivery", t.TempDir())
	started, err := app.AcceptRun(ctx, HandleMessageInput{SessionID: session.ID, Text: "start"})
	if err != nil {
		t.Fatalf("AcceptRun start: %v", err)
	}
	queued, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID:       session.ID,
		Client:          "telegram",
		ExternalKey:     "460252218",
		Text:            "queued",
		BusyMode:        BusyInputModeQueue,
		DeliveryAddress: json.RawMessage(`{"chat_id":460252218}`),
	})
	if err != nil {
		t.Fatalf("AcceptRun queued: %v", err)
	}
	if queued.Status != AcceptRunStatusQueued {
		t.Fatalf("queued status = %q, want queued", queued.Status)
	}
	if err := app.ExecuteRun(ctx, started.Run.ID); err != nil {
		t.Fatalf("ExecuteRun initial: %v", err)
	}

	pending, err := sqliteStore.ListPendingSessionInputs(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListPendingSessionInputs: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending inputs = %#v, want queue consumed", pending)
	}
	deliveries, err := app.ListClientDeliveries(ctx, ClientDeliveryFilter{
		Type: ClientDeliveryTypeRun,
	})
	if err != nil {
		t.Fatalf("ListClientDeliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %#v, want one queued run delivery", deliveries)
	}
	if deliveries[0].RunID == started.Run.ID || deliveries[0].RunID == "" {
		t.Fatalf("delivery RunID = %q, want consumed queued run", deliveries[0].RunID)
	}
	if deliveries[0].Summary != "queued" {
		t.Fatalf("delivery summary = %q, want queued input summary", deliveries[0].Summary)
	}
	if string(deliveries[0].Address) != `{"chat_id":460252218}` {
		t.Fatalf("delivery address = %s", deliveries[0].Address)
	}
}

func TestRecoverActiveRunsRestartsAcceptedRun(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	initialStarter := &recordingRunStarter{}
	app.WithRunStarter(initialStarter)

	session := saveSubagentTestSession(t, sqliteStore, "session_recover_accepted", "Recover accepted", t.TempDir())
	started, err := app.AcceptRun(ctx, HandleMessageInput{SessionID: session.ID, Text: "start"})
	if err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}
	if len(initialStarter.calls) != 1 {
		t.Fatalf("initial starter calls = %#v, want accepted run started once", initialStarter.calls)
	}

	recovered := New(sqliteStore)
	recoveryStarter := &recordingRunStarter{}
	recovered.WithRunStarter(recoveryStarter)

	if err := recovered.RecoverActiveRuns(ctx); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	if len(recoveryStarter.calls) != 1 || recoveryStarter.calls[0] != started.Run.ID {
		t.Fatalf("recovery starter calls = %#v, want %q", recoveryStarter.calls, started.Run.ID)
	}
}

func TestCompletedRunStartsNextQueuedInputFIFO(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	app.WithSessionLLMs(&subagentLLMs{runtime: &recordingRuntime{response: providers.Response{Text: "first done"}}})

	session := saveSubagentTestSession(t, sqliteStore, "session_fifo", "FIFO", t.TempDir())
	started, err := app.AcceptRun(ctx, HandleMessageInput{SessionID: session.ID, Text: "start"})
	if err != nil {
		t.Fatalf("AcceptRun start: %v", err)
	}
	if _, err := app.AcceptRun(ctx, HandleMessageInput{SessionID: session.ID, Text: "queued one", BusyMode: BusyInputModeQueue}); err != nil {
		t.Fatalf("AcceptRun queued one: %v", err)
	}
	if _, err := app.AcceptRun(ctx, HandleMessageInput{SessionID: session.ID, Text: "queued two", BusyMode: BusyInputModeQueue}); err != nil {
		t.Fatalf("AcceptRun queued two: %v", err)
	}

	if err := app.ExecuteRun(ctx, started.Run.ID); err != nil {
		t.Fatalf("ExecuteRun initial: %v", err)
	}

	if len(starter.calls) != 2 {
		t.Fatalf("starter calls = %#v, want initial run plus one queued run", starter.calls)
	}
	messages, err := sqliteStore.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	last := messages[len(messages)-1]
	if last.Role != MessageRoleUser || last.Content != "queued one" {
		t.Fatalf("last message = %#v, want first queued user message", last)
	}
	pending, err := sqliteStore.ListPendingSessionInputs(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListPendingSessionInputs: %v", err)
	}
	if len(pending) != 1 || pending[0].Text != "queued two" {
		t.Fatalf("pending inputs = %#v, want second queued input still pending", pending)
	}
}

func TestBusySteerAppendsGuidanceToToolResult(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	runtime := &steerToolRuntime{}
	app.WithRunStarter(starter)
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithTools(tools.NewRegistry(steerEchoTool{}))

	session := saveSubagentTestSession(t, sqliteStore, "session_steer", "Steer", t.TempDir())
	started, err := app.AcceptRun(ctx, HandleMessageInput{SessionID: session.ID, Text: "start"})
	if err != nil {
		t.Fatalf("AcceptRun start: %v", err)
	}
	steered, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID: session.ID,
		Text:      "use blue",
		BusyMode:  BusyInputModeSteer,
	})
	if err != nil {
		t.Fatalf("AcceptRun steer: %v", err)
	}
	if steered.Status != AcceptRunStatusSteered {
		t.Fatalf("steer status = %q, want steered", steered.Status)
	}

	if err := app.ExecuteRun(ctx, started.Run.ID); err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
	if !runtime.sawGuidance {
		t.Fatalf("runtime did not receive steered tool result in next provider request")
	}
	messages, err := sqliteStore.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if !messagesContain(messages, "User guidance: use blue") {
		t.Fatalf("messages missing injected guidance: %#v", messages)
	}
	pending, err := sqliteStore.ListPendingSessionInputs(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListPendingSessionInputs: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending inputs = %#v, want steer consumed", pending)
	}
}

func TestBusyInterruptCancelsActiveRunAndStartsInterruptInput(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)

	session := saveSubagentTestSession(t, sqliteStore, "session_interrupt", "Interrupt", t.TempDir())
	started, err := app.AcceptRun(ctx, HandleMessageInput{SessionID: session.ID, Text: "start"})
	if err != nil {
		t.Fatalf("AcceptRun start: %v", err)
	}

	interrupted, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID: session.ID,
		Text:      "stop and do this",
		BusyMode:  BusyInputModeInterrupt,
	})
	if err != nil {
		t.Fatalf("AcceptRun interrupt: %v", err)
	}
	if interrupted.Status != AcceptRunStatusInterrupting {
		t.Fatalf("interrupt status = %q, want interrupting", interrupted.Status)
	}
	run, err := sqliteStore.GetRun(ctx, started.Run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != RunStatusCanceled {
		t.Fatalf("active run status = %q, want canceled", run.Status)
	}
	if len(starter.calls) != 2 {
		t.Fatalf("starter calls = %#v, want interrupt input started after cancel", starter.calls)
	}
	messages, err := sqliteStore.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	last := messages[len(messages)-1]
	if last.Role != MessageRoleUser || last.Content != "stop and do this" {
		t.Fatalf("last message = %#v, want interrupt user message", last)
	}
}

type steerToolRuntime struct {
	calls       int
	sawGuidance bool
}

func (r *steerToolRuntime) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	r.calls++
	if r.calls == 1 {
		return providers.Response{ToolCalls: []providers.ToolCall{{
			ID:        "tool_echo",
			Name:      "echo",
			Arguments: json.RawMessage(`{"text":"tool output"}`),
		}}}, nil
	}
	r.sawGuidance = requestHasToolResultContent(request, "User guidance: use blue")
	if r.sawGuidance {
		return providers.Response{Text: "applied guidance"}, nil
	}
	return providers.Response{Text: "missing guidance"}, nil
}

type steerEchoTool struct{}

func (steerEchoTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              "echo",
		Name:            "echo",
		Description:     "Echo test input",
		Namespace:       "test",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Category:        tools.CategoryStorage,
		Profiles:        []tools.Profile{tools.ProfileCoding},
		OutputKind:      tools.OutputText,
		InputJSONSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
	}
}

func (steerEchoTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	var args struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(call.Args, &args)
	if strings.TrimSpace(args.Text) == "" {
		args.Text = "tool output"
	}
	return tools.Result{Content: args.Text}, nil
}
