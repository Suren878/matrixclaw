package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func newApprovalApp(t *testing.T, ids ...string) *appModel {
	t.Helper()
	m := newApp(context.Background(), nil)
	snapshot := core.ClientSnapshot{SessionID: "session_1"}
	for _, id := range ids {
		snapshot.Approvals = append(snapshot.Approvals, core.Approval{ID: id, SessionID: "session_1", ToolCallRef: "call_" + id, ToolName: "bash", State: core.ApprovalStatePending})
	}
	m.read = readmodel.New(snapshot)
	m.syncPermissionDialogCmd()
	return m
}

// openDenyPrompt presses "deny with reason" on the permission dialog in front.
func openDenyPrompt(t *testing.T, m *appModel, id string) {
	t.Helper()
	m.handleDialogInput(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if got := m.denyingApproval(); got != id {
		t.Fatalf("deny prompt for %q, want %q", got, id)
	}
}

func permissionShown(m *appModel) string {
	dialog, ok := m.dialog.DialogLast().(*surfacedialog.Permissions)
	if !ok {
		return ""
	}
	return dialog.Permission().ID
}

func TestFailedDenialAsksAboutTheApprovalAgain(t *testing.T) {
	m := newApprovalApp(t, "a1")
	openDenyPrompt(t, m, "a1")
	m.syncPermissionDialogCmd()
	if m.denyingApproval() != "a1" {
		t.Fatal("a sync replaced the reason prompt")
	}

	m.handleDialogInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog.ContainsDialog(surfacedialog.PromptCommandID) || permissionShown(m) != "" {
		t.Fatal("reason prompt or approval still shown after submitting")
	}
	m.Update(controlplaneResultMsg{command: "/approval deny a1 not now", seq: m.controlplaneSeq, err: errors.New("daemon unavailable")})

	if got := permissionShown(m); got != "a1" {
		t.Fatalf("approval shown after the failure = %q", got)
	}
}

func TestReasonPromptClosesWhenTheApprovalIsDecidedElsewhere(t *testing.T) {
	m := newApprovalApp(t, "a1")
	openDenyPrompt(t, m, "a1")

	payload, _ := json.Marshal(core.PermissionNotification{ApprovalID: "a1", ToolCallID: "call_a1", Granted: true})
	if err := m.read.Apply(daemonclient.LiveEvent{Type: core.EventApprovalResult, SessionID: "session_1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	m.syncPermissionDialogCmd()

	if m.dialog.ContainsDialog(surfacedialog.PromptCommandID) {
		t.Fatal("reason prompt kept for a decided approval")
	}
}

func TestClosedReasonPromptDoesNotHoldBackOtherApprovals(t *testing.T) {
	m := newApprovalApp(t, "a1", "a2")
	openDenyPrompt(t, m, "a1")

	m.closeAllDialogs()
	m.syncPermissionDialogCmd()

	if got := permissionShown(m); got != "a2" {
		t.Fatalf("approval shown = %q, want a2", got)
	}
}
