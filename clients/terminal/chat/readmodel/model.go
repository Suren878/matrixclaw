// Package readmodel is the terminal's view of one session: built from a daemon
// snapshot and updated in place by live events. It is used from the Bubble Tea
// loop only; accessors return the model's own values, which callers must not
// change.
package readmodel

import (
	"cmp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// Model is one session's state as the terminal shows it.
type Model struct {
	sessionID    string
	session      *core.Session
	capabilities *core.SessionCapabilities
	context      *core.ContextReport
	todo         *todo.List
	run          *core.Run
	// lastEventAt is when the session last showed activity, by daemon clock.
	lastEventAt time.Time

	// order lists message IDs as first seen; entries hold their conversion.
	order   []string
	entries map[string]*entry
	// resultRevs is the revision of each tool call's result message.
	resultRevs map[string]uint64
	visible    []surfacemessage.Message
	stale      bool

	toolUpdates   map[string]core.ToolUpdate
	approvals     map[string]surfacepermission.PermissionRequest
	notifications map[string]surfacepermission.PermissionNotification
	subagents     map[string]surfacemessage.Subagent
	inputs        map[string]core.SessionInput

	// Sorted views of the maps, rebuilt after a change.
	toolUpdateList   []core.ToolUpdate
	approvalList     []surfacepermission.PermissionRequest
	notificationList []surfacepermission.PermissionNotification
	subagentList     []surfacemessage.Subagent
	inputList        []core.SessionInput
	subagentCreated  map[string]int64
	inputCreated     map[string]int64
}

// revisions numbers message changes across all models, so a row built from an
// older model never matches a newer model's revision.
var revisions atomic.Uint64

type entry struct {
	message surfacemessage.Message
	shown   bool
	rev     uint64
}

// New builds the model from a snapshot.
func New(snapshot core.ClientSnapshot) *Model {
	m := &Model{
		sessionID:       snapshot.SessionID,
		session:         snapshot.Session,
		capabilities:    snapshot.Capabilities,
		context:         snapshot.Context,
		todo:            snapshot.Todo,
		run:             snapshot.Run,
		entries:         map[string]*entry{},
		resultRevs:      map[string]uint64{},
		toolUpdates:     map[string]core.ToolUpdate{},
		approvals:       map[string]surfacepermission.PermissionRequest{},
		notifications:   map[string]surfacepermission.PermissionNotification{},
		subagents:       map[string]surfacemessage.Subagent{},
		inputs:          map[string]core.SessionInput{},
		subagentCreated: map[string]int64{},
		inputCreated:    map[string]int64{},
	}
	for _, message := range snapshot.Messages {
		m.upsertMessage(message)
	}
	for _, update := range snapshot.ToolUpdates {
		if update.ToolCallID != "" {
			m.toolUpdates[update.ToolCallID] = update
		}
	}
	for _, approval := range snapshot.Approvals {
		if request := permissionRequest(approval); request.ID != "" {
			m.approvals[request.ID] = request
		}
	}
	for _, notification := range snapshot.ApprovalNotifications {
		if notification.ToolCallID != "" {
			m.notifications[notification.ToolCallID] = permissionNotification(notification)
		}
	}
	for _, task := range snapshot.Subagents {
		m.putSubagent(task)
	}
	for _, input := range snapshot.PendingInputs {
		m.putInput(input)
	}
	m.sortTools()
	m.sortApprovals()
	m.sortSubagents()
	m.sortInputs()
	if snapshot.Timing != nil {
		m.lastEventAt = snapshot.Timing.LastEventAt
	}
	return m
}

// Apply folds a live event of the model's session into the model.
func (m *Model) Apply(event daemonclient.LiveEvent) error {
	if event.SessionID != "" && event.SessionID != m.sessionID {
		return nil
	}
	if event.At.After(m.lastEventAt) {
		m.lastEventAt = event.At
	}
	switch event.Type {
	case core.EventMessageCreated, core.EventMessageUpdated:
		message, err := event.DecodeMessage()
		if err != nil {
			return err
		}
		m.upsertMessage(message)
	case core.EventApprovalRequest:
		request, err := event.DecodePermissionRequest()
		if err != nil {
			return err
		}
		m.approvals[request.ID] = toSurfacePermissionRequest(request)
		delete(m.notifications, request.ToolCallID)
		m.sortApprovals()
	case core.EventApprovalResult:
		notification, err := event.DecodePermissionNotification()
		if err != nil {
			return err
		}
		delete(m.approvals, notification.ApprovalID)
		for id, approval := range m.approvals {
			if approval.ToolCallID == notification.ToolCallID {
				delete(m.approvals, id)
			}
		}
		if notification.ToolCallID != "" {
			m.notifications[notification.ToolCallID] = permissionNotification(notification)
		}
		m.sortApprovals()
	case core.EventToolUpdated:
		update, err := event.DecodeToolUpdate()
		if err != nil {
			return err
		}
		if update.ToolCallID != "" {
			m.toolUpdates[update.ToolCallID] = update
			m.sortTools()
		}
	case core.EventRunUpdated:
		run, err := event.DecodeRun()
		if err != nil {
			return err
		}
		m.run = &run
	case core.EventTodoUpdated:
		list, err := event.DecodeTodo()
		if err != nil {
			return err
		}
		m.todo = &list
	case core.EventContextUpdated:
		usage, err := event.DecodeContextUsage()
		if err != nil {
			return err
		}
		next := core.ContextReport{SessionID: usage.SessionID}
		if m.context != nil {
			next = *m.context
		}
		next.TokenEstimate, next.WindowTokens = usage.TokenEstimate, usage.WindowTokens
		m.context = &next
	case core.EventTaskUpdated:
		task, err := event.DecodeTask()
		if err != nil {
			return err
		}
		m.putSubagent(task)
		m.sortSubagents()
	case core.EventInputUpdated:
		input, err := event.DecodeSessionInput()
		if err != nil {
			return err
		}
		m.putInput(input)
		m.sortInputs()
	}
	return nil
}

func (m *Model) SessionID() string                  { return m.sessionID }
func (m *Model) Session() *core.Session             { return m.session }
func (m *Model) Context() *core.ContextReport       { return m.context }
func (m *Model) Todo() *todo.List                   { return m.todo }
func (m *Model) Run() *core.Run                     { return m.run }
func (m *Model) LastEventAt() time.Time             { return m.lastEventAt }
func (m *Model) ToolUpdates() []core.ToolUpdate     { return m.toolUpdateList }
func (m *Model) PendingInputs() []core.SessionInput { return m.inputList }

// Capabilities are the session's own, or derived from the session when the
// snapshot had none.
func (m *Model) Capabilities() core.SessionCapabilities {
	switch {
	case m.capabilities != nil:
		return *m.capabilities
	case m.session != nil:
		return core.CapabilitiesForSession(*m.session)
	default:
		return core.SessionCapabilities{ProviderSelection: true, PermissionMode: true, NativeTools: true}
	}
}

// Approvals are the pending approvals ordered by path, then ID.
func (m *Model) Approvals() []surfacepermission.PermissionRequest { return m.approvalList }

// ApprovalNotifications are the decided approvals, one per tool call.
func (m *Model) ApprovalNotifications() []surfacepermission.PermissionNotification {
	return m.notificationList
}

// Subagents are the session's child agents, oldest first.
func (m *Model) Subagents() []surfacemessage.Subagent { return m.subagentList }

// Messages are the messages the transcript shows, in the order they arrived;
// a change yields a new slice, so items may keep pointers into an old one.
func (m *Model) Messages() []surfacemessage.Message {
	if m.stale {
		m.visible = make([]surfacemessage.Message, 0, len(m.order))
		for _, id := range m.order {
			if e := m.entries[id]; e.shown {
				m.visible = append(m.visible, e.message)
			}
		}
		m.stale = false
	}
	return m.visible
}

// Revision identifies the last change of the message with id, 0 for an
// unknown message.
func (m *Model) Revision(id string) uint64 {
	if e, ok := m.entries[id]; ok {
		return e.rev
	}
	return 0
}

// ResultRevision is the revision of the message holding the result of the
// tool call callID, 0 before it has one.
func (m *Model) ResultRevision(callID string) uint64 {
	return m.resultRevs[callID]
}

func (m *Model) upsertMessage(message transcript.Message) {
	if message.ID == "" || message.Origin == transcript.OriginEngineModel {
		return
	}
	surface := ToSurfaceMessage(message)
	if surface.Role == surfacemessage.Assistant && m.session != nil {
		surface.Provider = cmp.Or(strings.TrimSpace(surface.Provider), strings.TrimSpace(m.session.ProviderID))
		surface.Model = cmp.Or(strings.TrimSpace(surface.Model), strings.TrimSpace(m.session.ModelID))
	}
	rev := revisions.Add(1)
	e, ok := m.entries[message.ID]
	if !ok {
		e = &entry{}
		m.entries[message.ID] = e
		m.order = append(m.order, message.ID)
	}
	e.message, e.shown, e.rev = surface, shouldKeepSurfaceMessage(surface), rev
	for _, result := range surface.ToolResults() {
		m.resultRevs[result.ToolCallID] = rev
	}
	m.stale = true
}

func (m *Model) putSubagent(task core.Task) {
	if task.ID == "" || task.Kind != core.TaskKindSubagent {
		return
	}
	m.subagents[task.ID] = subagentFrom(task)
	m.subagentCreated[task.ID] = task.StartedAt.UnixNano()
}

func (m *Model) putInput(input core.SessionInput) {
	if input.ID == "" {
		return
	}
	if input.Status != core.SessionInputStatusPending {
		delete(m.inputs, input.ID)
		return
	}
	m.inputs[input.ID] = input
	m.inputCreated[input.ID] = input.CreatedAt.UnixNano()
}

func (m *Model) sortTools() {
	m.toolUpdateList = sortedValues(m.toolUpdates, func(a, b core.ToolUpdate) int { return strings.Compare(a.ToolCallID, b.ToolCallID) })
}

func (m *Model) sortApprovals() {
	m.approvalList = sortedValues(m.approvals, func(a, b surfacepermission.PermissionRequest) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	m.notificationList = sortedValues(m.notifications, func(a, b surfacepermission.PermissionNotification) int {
		return strings.Compare(a.ToolCallID, b.ToolCallID)
	})
}

func (m *Model) sortSubagents() {
	m.subagentList = sortedValues(m.subagents, func(a, b surfacemessage.Subagent) int {
		return compareCreated(m.subagentCreated[a.ID], m.subagentCreated[b.ID], a.ID, b.ID)
	})
}

func (m *Model) sortInputs() {
	m.inputList = sortedValues(m.inputs, func(a, b core.SessionInput) int {
		return compareCreated(m.inputCreated[a.ID], m.inputCreated[b.ID], a.ID, b.ID)
	})
}

func compareCreated(a, b int64, idA, idB string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return strings.Compare(idA, idB)
	}
}

func sortedValues[K comparable, V any](values map[K]V, compare func(a, b V) int) []V {
	out := make([]V, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	slices.SortFunc(out, compare)
	return out
}
