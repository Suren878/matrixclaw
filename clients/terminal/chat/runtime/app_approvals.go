package runtime

import (
	"encoding/json"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (m *appModel) syncPermissionDialogCmd() tea.Cmd {
	if m.read == nil || m.dialog == nil {
		return nil
	}
	if denying := m.denyingApproval(); denying != "" {
		if m.approvalPending(denying) {
			return nil
		}
		// Decided elsewhere: the reason prompt has nothing left to deny.
		m.dialog.CloseDialog(surfacedialog.PromptCommandID)
	}
	pending := m.pendingApprovals()
	if len(pending) == 0 {
		m.dialog.CloseDialog(surfacedialog.PermissionsID)
		return nil
	}
	current, ok := m.dialog.Dialog(surfacedialog.PermissionsID).(*surfacedialog.Permissions)
	if ok {
		for _, approval := range pending {
			if approval.ID == current.Permission().ID {
				return nil
			}
		}
		m.dialog.CloseDialog(surfacedialog.PermissionsID)
	}

	m.dialog.OpenDialog(surfacedialog.NewPermissions(m.com, pending[0]))
	return nil
}

// denyingApproval is the approval whose denial reason is being typed: the one
// the deny prompt on top of the dialogs asks about.
func (m *appModel) denyingApproval() string {
	prompt, ok := m.dialog.DialogLast().(*surfacedialog.PromptCommand)
	if !ok {
		return ""
	}
	approvalID, _ := controlplane.DeniedApproval(prompt.SubmitCommandPrefix())
	return approvalID
}

func (m *appModel) approvalPending(approvalID string) bool {
	for _, approval := range m.state().Approvals() {
		if approval.ID == approvalID {
			return true
		}
	}
	return false
}

func (m *appModel) pendingApprovals() []surfacepermission.PermissionRequest {
	approvals := m.state().Approvals()
	pending := make([]surfacepermission.PermissionRequest, 0, len(approvals))
	for _, approval := range approvals {
		if _, suppressed := m.suppressedApprovals[approval.ID]; suppressed {
			continue
		}
		pending = append(pending, approval)
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].Path == pending[j].Path {
			return pending[i].ID < pending[j].ID
		}
		return pending[i].Path < pending[j].Path
	})
	return pending
}

func (m *appModel) pruneSuppressedApprovals() {
	if len(m.suppressedApprovals) == 0 || m.read == nil {
		return
	}
	active := map[string]struct{}{}
	for _, approval := range m.state().Approvals() {
		active[approval.ID] = struct{}{}
	}
	for id := range m.suppressedApprovals {
		if _, ok := active[id]; !ok {
			delete(m.suppressedApprovals, id)
		}
	}
}

func (m *appModel) resolveApprovalCmd(permission surfacepermission.PermissionRequest, request core.ApprovalResolveRequest) tea.Cmd {
	if m.rt == nil {
		return nil
	}
	return func() tea.Msg {
		approval, err := m.rt.ResolveApproval(m.ctx, permission.ID, request)
		return resolveApprovalMsg{
			approval:   approval,
			approved:   request.Approved,
			approvalID: permission.ID,
			err:        err,
		}
	}
}

func (m *appModel) cancelRunCmd(runID string) tea.Cmd {
	if m.rt == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	return func() tea.Msg {
		run, err := m.rt.cancelRun(m.ctx, runID)
		return cancelRunResultMsg{run: run, err: err}
	}
}

func (m *appModel) openCancelRunDialog() tea.Cmd {
	run := m.state().Run()
	if !runIsActive(run) || strings.TrimSpace(run.ID) == "" {
		return nil
	}
	if m.dialog.ContainsDialog(surfacedialog.ConfirmRunCancelID) {
		m.dialog.BringToFront(surfacedialog.ConfirmRunCancelID)
		return nil
	}
	m.dialog.OpenDialog(surfacedialog.NewConfirmRunCancel(m.com, run.ID))
	return nil
}

func (m *appModel) applyResolvedApproval(msg resolveApprovalMsg) {
	if m.read == nil {
		return
	}

	sessionID := strings.TrimSpace(msg.approval.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(m.session)
	}

	payload, err := json.Marshal(core.PermissionNotification{
		ApprovalID: msg.approvalID,
		ToolCallID: msg.approval.ToolCallRef,
		Granted:    msg.approved,
		Denied:     !msg.approved,
	})
	if err != nil {
		m.err = err.Error()
		return
	}

	if err := m.read.Apply(daemonclient.LiveEvent{
		Type:      core.EventApprovalResult,
		SessionID: sessionID,
		RunID:     msg.approval.RunID,
		Payload:   payload,
	}); err != nil {
		m.err = err.Error()
		return
	}

	m.rebuildChat()
}
