package runtime

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
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
	ctx := m.streamCtx
	return func() tea.Msg {
		events, errs, err := m.rt.subscribeEvents(ctx, sessionID, afterID)
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

func (m *appModel) applySnapshot(snapshot core.ClientSnapshot) {
	m.clearContextCompactProgress()
	previousSessionID := strings.TrimSpace(m.session)
	nextSessionID := strings.TrimSpace(snapshot.SessionID)
	sessionChanged := previousSessionID != "" && nextSessionID != "" && previousSessionID != nextSessionID
	if sessionChanged {
		m.transientMessages = nil
		m.todoPanel = todoPanelAuto
	}
	m.restartStream()
	if previousSessionID != nextSessionID {
		m.lastEventID = 0
	}
	m.err = snapshotError(snapshot)
	m.session = snapshot.SessionID
	m.read = readmodel.New(snapshot)
	m.setBusy(runIsActive(snapshot.Run))
	m.syncChat()
}

// restartStream closes the current event stream and prepares the context for the next one;
// messages from the closed stream are ignored once streamID moves on.
func (m *appModel) restartStream() {
	m.stopStream()
	m.streamCtx, m.cancelStream = context.WithCancel(m.ctx)
}

func (m *appModel) stopStream() {
	if m.cancelStream != nil {
		m.cancelStream()
	}
	m.streamCtx, m.cancelStream = nil, nil
	m.events, m.eventErr = nil, nil
	m.streamID++
}

func snapshotError(snapshot core.ClientSnapshot) string {
	if snapshot.Run == nil || snapshot.Run.Status != core.RunStatusFailed {
		return ""
	}
	return strings.TrimSpace(snapshot.Run.Error)
}

// noSession stands in for the read model before the first snapshot.
var noSession = readmodel.New(core.ClientSnapshot{})

// state is the session's read model, empty before the first snapshot.
func (m *appModel) state() *readmodel.Model {
	if m.read == nil {
		return noSession
	}
	return m.read
}

// currentSessionLLM is the session's provider and model; an external agent
// session names only its model.
func (m *appModel) currentSessionLLM() (string, string) {
	session := m.state().Session()
	if session == nil {
		return "", ""
	}
	if core.CoreSessionIsExternalAgent(*session) {
		return "", strings.TrimSpace(session.ModelID)
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

// syncPromptHistory fills the prompt history newest first, so Up recalls the latest prompt.
func (m *appModel) syncPromptHistory() {
	messages := m.state().Messages()
	prompts := make([]string, 0, len(messages))
	for _, msg := range slices.Backward(messages) {
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
	case core.RunStatusAccepted, core.RunStatusRunning, core.RunStatusWaitingApproval, core.RunStatusWaitingEvents:
		return true
	default:
		return false
	}
}
