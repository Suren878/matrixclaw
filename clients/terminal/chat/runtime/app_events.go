package runtime

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (m *appModel) loadInitialCmd() tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.rt.loadOrInitSnapshot(m.ctx)
		return loadInitialMsg{snapshot: snapshot, err: err}
	}
}

func (m *appModel) subscribeCmd(sessionID string, streamID uint64, afterID uint64) tea.Cmd {
	return func() tea.Msg {
		events, errs, err := m.rt.subscribeEvents(m.ctx, sessionID, afterID)
		return subscribeReadyMsg{
			sessionID: sessionID,
			streamID:  streamID,
			events:    events,
			errs:      errs,
			err:       err,
		}
	}
}

func (m *appModel) waitEventCmd(streamID uint64, events <-chan daemonclient.LiveEvent, errs <-chan error) tea.Cmd {
	if events == nil && errs == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
			return liveEventMsg{streamID: streamID, done: true}
		case err, ok := <-errs:
			if !ok {
				return liveEventMsg{streamID: streamID, done: true}
			}
			if err != nil {
				return liveEventMsg{streamID: streamID, err: err}
			}
			return liveEventMsg{streamID: streamID, done: true}
		case event, ok := <-events:
			if !ok {
				return liveEventMsg{streamID: streamID, done: true}
			}
			return liveEventMsg{streamID: streamID, event: event}
		}
	}
}

func (m *appModel) reconnectCmd() tea.Cmd {
	return tea.Tick(reconnectDelay, func(time.Time) tea.Msg {
		return reconnectMsg{}
	})
}

func (m *appModel) applySnapshot(snapshot core.ClientSnapshot, restartStream bool) {
	m.clearContextCompactProgress()
	if restartStream {
		m.streamID++
		if strings.TrimSpace(m.session) != strings.TrimSpace(snapshot.SessionID) {
			m.lastEventID = 0
		}
	}
	m.err = snapshotError(snapshot)
	m.session = snapshot.SessionID
	m.read = viewmodel.NewReadModel(snapshot)
	m.setBusy(runIsActive(snapshot.Run))
	m.rebuildChat()
}

func snapshotError(snapshot core.ClientSnapshot) string {
	if snapshot.Run == nil || snapshot.Run.Status != core.RunStatusFailed {
		return ""
	}
	return strings.TrimSpace(snapshot.Run.Error)
}

func (m *appModel) currentRun() *core.Run {
	if m.read == nil {
		return nil
	}
	return cloneRun(m.read.Snapshot().Run)
}

func (m *appModel) currentModelLabel() string {
	_, sessionModel := m.currentSessionLLM()
	if sessionModel != "" {
		return sessionModel
	}
	if m.read != nil {
		snapshot := m.read.Snapshot()
		for i := len(snapshot.Messages) - 1; i >= 0; i-- {
			if label := strings.TrimSpace(snapshot.Messages[i].Model); label != "" {
				return label
			}
		}
	}
	if label := strings.TrimSpace(m.providerModel); label != "" {
		return label
	}
	if label := strings.TrimSpace(m.providerName); label != "" {
		return label
	}
	return ""
}

func (m *appModel) currentSessionLLM() (string, string) {
	if m.read == nil {
		return "", ""
	}
	session := m.read.Snapshot().Session
	if session == nil {
		return "", ""
	}
	return strings.TrimSpace(session.ProviderID), strings.TrimSpace(session.ModelID)
}

func (m *appModel) setBusy(busy bool) {
	m.busy = busy
	m.input.SetWorking(busy)
}

func (m *appModel) workingTickCmd() tea.Cmd {
	return tea.Tick(workingStatusTickInterval, func(at time.Time) tea.Msg {
		return workingTickMsg{at: at}
	})
}

func (m *appModel) syncPromptHistory() {
	if m.read == nil {
		return
	}
	snapshot := m.read.Snapshot()
	prompts := make([]string, 0, len(snapshot.Messages))
	for _, msg := range snapshot.Messages {
		if msg.Role != surfacemessage.User {
			continue
		}
		text := strings.TrimSpace(msg.Content().Text)
		if text == "" {
			continue
		}
		prompts = append(prompts, text)
	}
	m.input.SetPromptHistory(prompts)
}

func runIsActive(run *core.Run) bool {
	if run == nil {
		return false
	}
	switch run.Status {
	case core.RunStatusAccepted, core.RunStatusRunning, core.RunStatusWaitingApproval:
		return true
	default:
		return false
	}
}

func cloneRun(run *core.Run) *core.Run {
	if run == nil {
		return nil
	}
	cloned := *run
	return &cloned
}
