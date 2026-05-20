package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestPlanEnterOnTopLevelTaskOpensTaskMenu(t *testing.T) {
	now := time.Now().UTC()
	model := appWithPlanForKeyTest(now)
	model.planSelected = 0

	next, cmd := model.handlePlanKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected menu to open without command")
	}
	if !model.dialog.ContainsDialog(surfacedialog.CommandsID) {
		t.Fatal("expected task command menu")
	}
}

func TestPlanEnterOnSubtaskOpensTaskMenuWithoutNestedSubtask(t *testing.T) {
	now := time.Now().UTC()
	model := appWithPlanForKeyTest(now)
	model.planSelected = 1

	next, cmd := model.handlePlanKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no command")
	}
	if !model.dialog.ContainsDialog(surfacedialog.CommandsID) {
		t.Fatal("expected task command menu")
	}
	dialog := model.dialog.Dialog(surfacedialog.CommandsID)
	if dialog == nil {
		t.Fatal("expected commands dialog")
	}
	action := dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	run, ok := action.(surfacedialog.ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("action = %T, want ActionRunControlplaneCommand", action)
	}
	if strings.HasPrefix(run.Command, "/plan prompt subtask ") {
		t.Fatalf("subtask menu should not create nested subtasks, got %q", run.Command)
	}
}

func TestPlanPromptEditOpensPrefilledPrompt(t *testing.T) {
	now := time.Now().UTC()
	model := appWithPlanForKeyTest(now)

	cmd, handled := model.handlePlanPromptCommand("/plan prompt edit 1")
	if !handled {
		t.Fatal("expected edit prompt command to be handled")
	}
	if cmd != nil {
		t.Fatal("expected prompt to open without command")
	}
	dialog := model.dialog.Dialog(surfacedialog.PromptCommandID)
	if dialog == nil {
		t.Fatal("expected edit prompt")
	}
	action := dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	run, ok := action.(surfacedialog.ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("action = %T, want ActionRunControlplaneCommand", action)
	}
	if run.Command != "/plan edit 1 top level" {
		t.Fatalf("command = %q, want prefilled edit command", run.Command)
	}
}

func TestPlanActionButtonsUseHorizontalSelection(t *testing.T) {
	now := time.Now().UTC()
	model := appWithPlanForKeyTest(now)
	model.planSelected = planActionRowIndex(model.currentSnapshot().Plan)

	_, _ = model.handlePlanKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if model.planSelected != planActionRowIndex(model.currentSnapshot().Plan) {
		t.Fatalf("planSelected = %d, want action row", model.planSelected)
	}
	if model.planActionSelected != 1 {
		t.Fatalf("planActionSelected = %d, want run button", model.planActionSelected)
	}

	_, _ = model.handlePlanKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if model.planActionSelected != 2 {
		t.Fatalf("planActionSelected = %d, want cancel button", model.planActionSelected)
	}

	_, _ = model.handlePlanKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if model.planActionSelected != 1 {
		t.Fatalf("planActionSelected = %d, want run button after left", model.planActionSelected)
	}
}

func TestStartPlanRunWhileBusyDoesNotStartSecondRun(t *testing.T) {
	now := time.Now().UTC()
	model := appWithPlanForKeyTest(now)
	model.session = "session-1"
	model.busy = true

	cmd := model.startPlanRunCmd()
	if cmd != nil {
		t.Fatal("expected no command while a run is already active")
	}
	if model.err != "agent is working, please wait" {
		t.Fatalf("err = %q, want busy message", model.err)
	}
}

func TestLoadInitialKeepsPlanFocusWhenPanelOpen(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(context.Background(), &Runtime{})
	model.width = 160
	model.height = 40
	model.planPanelOpen = true
	model.focus = appFocusPlan
	model.initialLoadComplete = true
	snapshot := snapshotWithPlanForKeyTest(now)

	next, _ := model.Update(loadInitialMsg{snapshot: snapshot})
	if next == nil {
		t.Fatal("expected model")
	}
	if model.focus != appFocusPlan {
		t.Fatalf("focus = %v, want appFocusPlan", model.focus)
	}
}

func appWithPlanForKeyTest(now time.Time) *appModel {
	model := newApp(context.Background(), &Runtime{})
	model.width = 160
	model.height = 40
	model.focus = appFocusPlan
	model.planPanelOpen = true
	model.read = viewmodel.NewReadModel(snapshotWithPlanForKeyTest(now))
	return model
}

func snapshotWithPlanForKeyTest(now time.Time) core.ClientSnapshot {
	snapshot := snapshotWithTexts(now, "ok")
	snapshot.Plan = &core.SessionPlan{
		SessionID: "session-1",
		Items: []core.PlanItem{
			{
				ID:        "task-1",
				SessionID: "session-1",
				Text:      "top level",
				Status:    core.PlanItemPending,
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "task-2",
				SessionID: "session-1",
				ParentID:  "task-1",
				Text:      "subtask",
				Status:    core.PlanItemPending,
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "task-3",
				SessionID: "session-1",
				ParentID:  "task-1",
				Text:      "second subtask",
				Status:    core.PlanItemPending,
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		UpdatedAt: now,
	}
	return snapshot
}
