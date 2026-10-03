package runtime

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

// stream is the live connection to the bound session: its events are
// subscribed first and the snapshot loaded second, so nothing between the two
// is lost; events the snapshot already covers are dropped.
type stream struct {
	ctx     context.Context
	cancel  context.CancelFunc
	id      uint64
	session string
	events  <-chan daemonclient.LiveEvent
	errs    <-chan error
	// loaded is set once the snapshot arrived; before that events wait in early.
	loaded bool
	early  []daemonclient.LiveEvent
	// floor is the snapshot's event ID: events up to it are already in it.
	floor uint64
}

type connectedMsg struct {
	streamID  uint64
	sessionID string
	events    <-chan daemonclient.LiveEvent
	errs      <-chan error
	err       error
}

type snapshotMsg struct {
	streamID uint64
	snapshot core.ClientSnapshot
	err      error
}

type liveEventMsg struct {
	streamID uint64
	event    daemonclient.LiveEvent
	err      error
	done     bool
}

type reconnectMsg struct{}

// reload drops the stream and connects again: the bound session's events,
// then its snapshot. It runs on start, reconnect and session switches.
func (m *appModel) reload() tea.Cmd {
	m.stopStream()
	m.loading = true
	m.stream.ctx, m.stream.cancel = context.WithCancel(m.ctx)
	ctx, id := m.stream.ctx, m.stream.id
	return func() tea.Msg {
		sessionID, err := m.rt.ensureSession(ctx)
		if err != nil {
			return connectedMsg{streamID: id, err: err}
		}
		events, errs, err := m.rt.subscribeEvents(ctx, sessionID)
		return connectedMsg{streamID: id, sessionID: sessionID, events: events, errs: errs, err: err}
	}
}

func (m *appModel) stopStream() {
	if m.stream.cancel != nil {
		m.stream.cancel()
	}
	m.stream = stream{id: m.stream.id + 1}
}

func (m *appModel) handleConnected(msg connectedMsg) tea.Cmd {
	if msg.streamID != m.stream.id {
		return nil
	}
	if msg.err != nil {
		return m.streamFailed(msg.err)
	}
	m.stream.session, m.stream.events, m.stream.errs = msg.sessionID, msg.events, msg.errs
	ctx, id := m.stream.ctx, m.stream.id
	loadSnapshot := func() tea.Msg {
		snapshot, err := m.rt.loadSnapshot(ctx)
		return snapshotMsg{streamID: id, snapshot: snapshot, err: err}
	}
	return tea.Batch(m.waitEventCmd(), loadSnapshot)
}

func (m *appModel) handleSnapshot(msg snapshotMsg) tea.Cmd {
	if msg.streamID != m.stream.id {
		return nil
	}
	if msg.err != nil {
		return m.streamFailed(msg.err)
	}
	if msg.snapshot.SessionID != m.stream.session {
		// The binding moved between subscribing and loading: follow it.
		return m.reload()
	}
	m.loading = false
	m.applySnapshot(msg.snapshot)
	m.stream.loaded, m.stream.floor = true, msg.snapshot.EventID
	early := m.stream.early
	m.stream.early = nil
	cmds := []tea.Cmd{m.syncPermissionDialogCmd(), m.setFocus(appFocusEditor), m.input.SetWidth(m.editorWidth())}
	for _, event := range early {
		if cmd := m.applyEvent(event); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	m.syncChat()
	return tea.Batch(cmds...)
}

// streamFailed shows err, if any, and once a session was shown tries again.
func (m *appModel) streamFailed(err error) tea.Cmd {
	m.loading = false
	m.setBusy(false)
	if err != nil {
		m.showError(err.Error())
	}
	m.stopStream()
	if m.read == nil {
		return nil
	}
	return tea.Tick(reconnectDelay, func(time.Time) tea.Msg { return reconnectMsg{} })
}

func (m *appModel) waitEventCmd() tea.Cmd {
	id, events, errs := m.stream.id, m.stream.events, m.stream.errs
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
			return liveEventMsg{streamID: id, done: true}
		case err, ok := <-errs:
			if ok && err != nil {
				return liveEventMsg{streamID: id, err: err}
			}
			return liveEventMsg{streamID: id, done: true}
		case event, ok := <-events:
			if !ok {
				return liveEventMsg{streamID: id, done: true}
			}
			return liveEventMsg{streamID: id, event: event}
		}
	}
}

func (m *appModel) handleLiveEvent(msg liveEventMsg) tea.Cmd {
	if msg.streamID != m.stream.id {
		return nil
	}
	if msg.err != nil {
		return m.streamFailed(msg.err)
	}
	if msg.done {
		return m.streamFailed(nil)
	}
	if !m.stream.loaded {
		m.stream.early = append(m.stream.early, msg.event)
		return m.waitEventCmd()
	}
	cmd := m.applyEvent(msg.event)
	m.syncChat()
	return tea.Batch(cmd, m.syncPermissionDialogCmd(), m.waitEventCmd())
}

// applyEvent folds one live event newer than the snapshot into the read model.
func (m *appModel) applyEvent(event daemonclient.LiveEvent) tea.Cmd {
	if event.ID != 0 && event.ID <= m.stream.floor {
		return nil
	}
	if event.SessionID != "" && event.SessionID != m.read.SessionID() {
		return nil
	}
	if err := m.read.Apply(event); err != nil {
		return m.streamFailed(err)
	}
	switch event.Type {
	case core.EventRunUpdated:
		run := m.read.Run()
		m.setBusy(runIsActive(run))
		m.showRunStopNotice(*run)
		if run.Status == core.RunStatusFailed && strings.TrimSpace(run.Error) != "" {
			m.showError(run.Error)
		}
	case core.EventInputUpdated:
		if input, err := event.DecodeSessionInput(); err == nil {
			m.showConsumedInputStatus(input)
		}
	}
	return nil
}

func (m *appModel) applySnapshot(snapshot core.ClientSnapshot) {
	m.clearContextCompactProgress()
	if m.read != nil && m.read.SessionID() != snapshot.SessionID {
		m.notes = nil
		m.todoPanel = todoPanelAuto
		m.chat, m.rows = nil, keptRows{}
	}
	m.showError(snapshotError(snapshot))
	m.read = readmodel.New(snapshot)
	m.setBusy(runIsActive(snapshot.Run))
	m.syncChat()
}

func snapshotError(snapshot core.ClientSnapshot) string {
	if snapshot.Run == nil || snapshot.Run.Status != core.RunStatusFailed {
		return ""
	}
	return strings.TrimSpace(snapshot.Run.Error)
}
