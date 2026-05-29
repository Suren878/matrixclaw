package runtime

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (m *appModel) handleLoadInitial(msg loadInitialMsg) tea.Cmd {
	m.loading = false
	if msg.err != nil {
		m.setBusy(false)
		m.events = nil
		m.eventErr = nil
		m.err = msg.err.Error()
		if strings.TrimSpace(m.session) != "" || m.lastEventID > 0 {
			return m.reconnectCmd()
		}
		return nil
	}
	firstLoad := !m.initialLoadComplete
	m.initialLoadComplete = true
	m.applySnapshot(msg.snapshot, true)
	if shouldShowStoredPlanSummary(msg.snapshot) {
		m.upsertTransientMessage(newPlanSummaryTransientMessageAt(renderPlanSummaryText("✅ Plan Finished", msg.snapshot.Plan), core.RunStatusCompleted, msg.snapshot.Plan.UpdatedAt))
		m.rebuildChat()
	}
	if m.planAutoRun && !runIsActive(msg.snapshot.Run) {
		if planHasOpenWork(msg.snapshot.Plan) {
			m.planPanelOpen = true
			return tea.Batch(
				m.syncPermissionDialogCmd(),
				m.subscribeCmd(msg.snapshot.SessionID, m.streamID, m.lastEventID),
				m.input.SetWidth(m.editorWidth()),
				m.startPlanRunCmd(),
			)
		}
		m.planAutoRun = false
	}
	shouldPromptPlanResume := firstLoad && !m.skipPlanResumeOnce && m.shouldPromptPlanResume(msg.snapshot)
	m.skipPlanResumeOnce = false
	if shouldPromptPlanResume {
		m.planResumePrompted = true
		m.dialog.OpenDialog(surfacedialog.NewConfirmCommand(m.com, surfacedialog.ConfirmCommandData{
			Message:        "Continue unfinished plan?",
			ConfirmLabel:   "Continue",
			CancelLabel:    "Cancel plan",
			ConfirmCommand: "/plan run",
			CancelCommand:  "/plan cancel",
			CancelDanger:   true,
		}))
	}
	focusCmd := m.setFocus(appFocusEditor)
	if m.planPanelOpen && m.availablePlanPanelWidth() > 0 {
		focusCmd = m.setFocus(appFocusPlan)
	}
	return tea.Batch(
		m.syncPermissionDialogCmd(),
		m.subscribeCmd(msg.snapshot.SessionID, m.streamID, m.lastEventID),
		focusCmd,
		m.input.SetWidth(m.editorWidth()),
	)
}

func (m *appModel) handleSubscribeReady(msg subscribeReadyMsg) tea.Cmd {
	if msg.streamID != m.streamID {
		return nil
	}
	if msg.err != nil {
		m.err = msg.err.Error()
		return m.reconnectCmd()
	}
	m.events = msg.events
	m.eventErr = msg.errs
	return m.waitEventCmd(msg.streamID, msg.events, msg.errs)
}

func (m *appModel) handleLiveEvent(msg liveEventMsg) tea.Cmd {
	if msg.streamID != m.streamID {
		return nil
	}
	if msg.err != nil {
		m.err = msg.err.Error()
		return m.reconnectCmd()
	}
	if msg.done {
		return m.reconnectCmd()
	}
	if sessionID := strings.TrimSpace(msg.event.SessionID); sessionID != "" && sessionID != strings.TrimSpace(m.session) {
		return m.waitEventCmd(msg.streamID, m.events, m.eventErr)
	}
	if m.read != nil {
		if msg.event.ID > m.lastEventID {
			m.lastEventID = msg.event.ID
		}
		if err := m.read.Apply(msg.event); err != nil {
			m.err = err.Error()
			return m.reconnectCmd()
		}
		if cmd := m.handleRunUpdatedEvent(msg); cmd != nil {
			return cmd
		}
		if cmd := m.handleInputUpdatedEvent(msg); cmd != nil {
			return cmd
		}
		m.rebuildChat()
	}
	return tea.Batch(m.syncPermissionDialogCmd(), m.waitEventCmd(msg.streamID, m.events, m.eventErr))
}

func (m *appModel) handleRunUpdatedEvent(msg liveEventMsg) tea.Cmd {
	if msg.event.Type != core.EventRunUpdated {
		return nil
	}
	run, err := msg.event.DecodeRun()
	if err != nil {
		return nil
	}
	m.setBusy(runIsActive(&run))
	if run.Status == core.RunStatusFailed && strings.TrimSpace(run.Error) != "" {
		m.err = run.Error
		m.planAutoRun = false
	}
	if !runIsActive(&run) {
		if run.Status == core.RunStatusFailed {
			return tea.Batch(m.loadInitialCmd(), m.waitEventCmd(msg.streamID, m.events, m.eventErr))
		}
		var focusCmd tea.Cmd
		if !m.planAutoRun {
			m.planPanelOpen = false
		}
		if m.focus == appFocusPlan {
			focusCmd = m.setFocus(appFocusEditor)
		}
		m.showPlanFinishedSummary(run)
		return tea.Batch(focusCmd, m.loadInitialCmd(), m.waitEventCmd(msg.streamID, m.events, m.eventErr))
	}
	return nil
}

func (m *appModel) handleInputUpdatedEvent(msg liveEventMsg) tea.Cmd {
	if msg.event.Type != core.EventInputUpdated {
		return nil
	}
	input, err := msg.event.DecodeSessionInput()
	if err != nil {
		return nil
	}
	m.showConsumedInputStatus(input)
	return nil
}

func (m *appModel) handleSendMessageResult(msg sendMessageResultMsg) tea.Cmd {
	if msg.err != nil {
		m.setBusy(false)
		m.err = msg.err.Error()
		if msg.planRun {
			m.planAutoRun = false
			return nil
		}
		m.restoreEditorDraft(msg.content, msg.attachments)
		return nil
	}
	m.session = msg.result.SessionID
	if msg.result.Status == core.AcceptRunStatusQueued ||
		msg.result.Status == core.AcceptRunStatusSteered ||
		msg.result.Status == core.AcceptRunStatusInterrupting {
		m.showAcceptedInputStatus(msg.result.Status)
		return m.loadInitialCmd()
	}
	if m.read == nil {
		m.read = viewmodel.NewReadModel(core.ClientSnapshot{
			SessionID: msg.result.SessionID,
			Messages:  []core.Message{msg.result.UserMessage},
			Run:       &msg.result.Run,
		})
	} else {
		m.applyAcceptedRunToReadModel(msg.result)
	}
	m.setBusy(runIsActive(&msg.result.Run))
	m.rebuildChat()
	return m.loadInitialCmd()
}

func (m *appModel) applyAcceptedRunToReadModel(result core.AcceptRunResult) {
	if payload, err := json.Marshal(result.UserMessage); err == nil {
		_ = m.read.Apply(daemonclient.LiveEvent{
			Type:      core.EventMessageCreated,
			SessionID: result.SessionID,
			RunID:     result.Run.ID,
			Payload:   payload,
		})
	}
	if runPayload, err := json.Marshal(result.Run); err == nil {
		_ = m.read.Apply(daemonclient.LiveEvent{
			Type:      core.EventRunUpdated,
			SessionID: result.SessionID,
			RunID:     result.Run.ID,
			Payload:   runPayload,
		})
	}
}
