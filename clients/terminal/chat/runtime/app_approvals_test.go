package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/updater"
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
	m.Update(controlplaneResultMsg{command: "/approval deny a1 not now", seq: m.dialog.seq, err: errors.New("daemon unavailable")})

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

// typeKeys presses each key 100ms after the previous one and returns the
// command of the last.
func typeKeys(m *appModel, clock *time.Time, keys ...tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	for _, key := range keys {
		*clock = clock.Add(100 * time.Millisecond)
		_, cmd = m.Update(key)
	}
	return cmd
}

func letters(text string) []tea.KeyPressMsg {
	keys := make([]tea.KeyPressMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return keys
}

func TestTypingWhenAnApprovalPopsUpDoesNotAnswerIt(t *testing.T) {
	var resolved []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/resolve") {
			resolved = append(resolved, strings.TrimSpace(string(raw)))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	m := newApp(context.Background(), New(Config{BaseURL: server.URL}))
	clock := at(0)
	m.dialog.now = func() time.Time { return clock }
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(connectedMsg{streamID: m.stream.id, sessionID: "session_1"})
	m.Update(snapshotMsg{streamID: m.stream.id, snapshot: core.ClientSnapshot{SessionID: "session_1", Session: renderSession(),
		Run: &core.Run{ID: "run_1", SessionID: "session_1", Status: core.RunStatusRunning}}})

	typeKeys(m, &clock, letters("please ")...)
	deliver(t, m, core.EventApprovalRequest, core.PermissionRequest{ID: "approval_1", SessionID: "session_1", ToolCallID: "call_1", ToolName: "bash",
		Params: []byte(`{"command":"rm -rf ~/projects"}`), Suggestion: &permission.Suggestion{Tool: "bash", Pattern: "rm *"}})
	typeKeys(m, &clock, append(letters("go on and also allow"), tea.KeyPressMsg{Code: tea.KeyEnter})...)
	if permissionShown(m) != "approval_1" {
		t.Fatal("typing answered the approval")
	}

	clock = clock.Add(time.Second)
	if cmd := typeKeys(m, &clock, tea.KeyPressMsg{Code: 'g', Text: "g"}, tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		cmd()
	}
	if len(resolved) != 1 || resolved[0] != `{"approved":true,"always":"global"}` {
		t.Fatalf("g, enter after a pause resolved %q", resolved)
	}
}

func TestTypingWhenTheUpdatePromptPopsUpDoesNotInstall(t *testing.T) {
	m := newApp(context.Background(), nil)
	clock := at(0)
	m.dialog.now = func() time.Time { return clock }
	m.Update(updateCheckMsg{update: updater.Update{Current: "1.0.0", Latest: "1.1.0"}, ok: true})

	// The commands are not run: an accepted prompt would install for real.
	for _, key := range letters("yes") {
		clock = clock.Add(100 * time.Millisecond)
		m.Update(key)
	}
	if m.dialog.ContainsDialog(updateInfoDialogID) || !m.dialog.ContainsDialog(surfacedialog.ConfirmCommandID) {
		t.Fatal("typing answered the update prompt")
	}
	clock = clock.Add(time.Second)
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !m.dialog.ContainsDialog(updateInfoDialogID) {
		t.Fatal("y after a pause did not start the update")
	}
}
