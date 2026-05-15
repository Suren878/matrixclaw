package clientruntime

import (
	"sort"
	"sync"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

type StateSnapshot struct {
	SessionID             string
	Session               *core.Session
	Capabilities          *core.SessionCapabilities
	Context               *core.ContextReport
	Plan                  *core.SessionPlan
	Run                   *core.Run
	Timing                *core.RunTiming
	Messages              []core.Message
	ToolUpdates           []core.ToolUpdate
	Approvals             []core.PermissionRequest
	ApprovalNotifications []core.PermissionNotification
	Files                 []core.FileSnapshot
}

type State struct {
	mu                    sync.RWMutex
	sessionID             string
	session               *core.Session
	capabilities          *core.SessionCapabilities
	context               *core.ContextReport
	plan                  *core.SessionPlan
	run                   *core.Run
	timing                *core.RunTiming
	messages              []core.Message
	toolUpdates           map[string]core.ToolUpdate
	approvals             map[string]core.PermissionRequest
	approvalNotifications map[string]core.PermissionNotification
	filesByPath           map[string][]core.FileSnapshot
}

func NewState(snapshot core.ClientSnapshot) *State {
	model := &State{
		sessionID:             snapshot.SessionID,
		session:               cloneSession(snapshot.Session),
		capabilities:          cloneSessionCapabilities(snapshot.Capabilities),
		context:               cloneContextReport(snapshot.Context),
		plan:                  cloneSessionPlan(snapshot.Plan),
		run:                   cloneRun(snapshot.Run),
		timing:                cloneTiming(snapshot.Timing),
		messages:              append([]core.Message(nil), snapshot.Messages...),
		toolUpdates:           map[string]core.ToolUpdate{},
		approvals:             map[string]core.PermissionRequest{},
		approvalNotifications: map[string]core.PermissionNotification{},
		filesByPath:           map[string][]core.FileSnapshot{},
	}
	for _, update := range snapshot.ToolUpdates {
		if update.ToolCallID != "" {
			model.toolUpdates[update.ToolCallID] = update
		}
	}
	for _, approval := range snapshot.Approvals {
		request := PermissionRequestFromApproval(approval)
		if request.ID != "" {
			model.approvals[request.ID] = request
		}
	}
	for _, notification := range snapshot.ApprovalNotifications {
		if notification.ToolCallID != "" {
			model.approvalNotifications[notification.ToolCallID] = notification
		}
	}
	for _, file := range snapshot.Files {
		model.filesByPath[file.Path] = append(model.filesByPath[file.Path], file)
	}
	return model
}

func (s *State) Snapshot() StateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := StateSnapshot{
		SessionID:    s.sessionID,
		Session:      cloneSession(s.session),
		Capabilities: cloneSessionCapabilities(s.capabilities),
		Context:      cloneContextReport(s.context),
		Plan:         cloneSessionPlan(s.plan),
		Run:          cloneRun(s.run),
		Timing:       cloneTiming(s.timing),
		Messages:     append([]core.Message(nil), s.messages...),
	}
	for _, update := range s.toolUpdates {
		out.ToolUpdates = append(out.ToolUpdates, update)
	}
	sort.SliceStable(out.ToolUpdates, func(i, j int) bool {
		return out.ToolUpdates[i].ToolCallID < out.ToolUpdates[j].ToolCallID
	})
	for _, approval := range s.approvals {
		out.Approvals = append(out.Approvals, approval)
	}
	sort.SliceStable(out.Approvals, func(i, j int) bool {
		return out.Approvals[i].ID < out.Approvals[j].ID
	})
	for _, notification := range s.approvalNotifications {
		out.ApprovalNotifications = append(out.ApprovalNotifications, notification)
	}
	sort.SliceStable(out.ApprovalNotifications, func(i, j int) bool {
		return out.ApprovalNotifications[i].ToolCallID < out.ApprovalNotifications[j].ToolCallID
	})
	paths := make([]string, 0, len(s.filesByPath))
	for path := range s.filesByPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		versions := append([]core.FileSnapshot(nil), s.filesByPath[path]...)
		sort.SliceStable(versions, func(i, j int) bool {
			if versions[i].Version != versions[j].Version {
				return versions[i].Version < versions[j].Version
			}
			return versions[i].ID < versions[j].ID
		})
		out.Files = append(out.Files, versions...)
	}
	return out
}

func cloneContextReport(report *core.ContextReport) *core.ContextReport {
	if report == nil {
		return nil
	}
	copy := *report
	copy.Blocks = append([]core.ContextBlock(nil), report.Blocks...)
	if report.LastProviderUsage != nil {
		usage := *report.LastProviderUsage
		if usage.ProviderRaw != nil {
			usage.ProviderRaw = append([]byte(nil), usage.ProviderRaw...)
		}
		copy.LastProviderUsage = &usage
	}
	return &copy
}

func cloneSessionPlan(plan *core.SessionPlan) *core.SessionPlan {
	if plan == nil {
		return nil
	}
	copy := *plan
	copy.Items = append([]core.PlanItem(nil), plan.Items...)
	return &copy
}

func (s *State) Apply(event daemonclient.LiveEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if eventSessionID := event.SessionID; eventSessionID != "" && eventSessionID != s.sessionID {
		return nil
	}

	switch event.Type {
	case core.EventMessageCreated, core.EventMessageUpdated:
		message, err := event.DecodeMessage()
		if err != nil {
			return err
		}
		s.upsertMessage(message)
	case core.EventApprovalRequest:
		request, err := event.DecodePermissionRequest()
		if err != nil {
			return err
		}
		s.approvals[request.ID] = request
		delete(s.approvalNotifications, request.ToolCallID)
	case core.EventApprovalResult:
		notification, err := event.DecodePermissionNotification()
		if err != nil {
			return err
		}
		if notification.ApprovalID != "" {
			delete(s.approvals, notification.ApprovalID)
		}
		for id, approval := range s.approvals {
			if approval.ToolCallID == notification.ToolCallID {
				delete(s.approvals, id)
			}
		}
		if notification.ToolCallID != "" {
			s.approvalNotifications[notification.ToolCallID] = notification
		}
	case core.EventToolUpdated:
		update, err := event.DecodeToolUpdate()
		if err != nil {
			return err
		}
		if update.ToolCallID != "" {
			s.toolUpdates[update.ToolCallID] = update
		}
	case core.EventRunUpdated:
		run, err := event.DecodeRun()
		if err != nil {
			return err
		}
		s.run = cloneRun(&run)
		s.timing = nil
	case core.EventPlanUpdated:
		plan, err := event.DecodeSessionPlan()
		if err != nil {
			return err
		}
		s.plan = cloneSessionPlan(&plan)
	case core.EventFileVersioned:
		file, err := event.DecodeFileSnapshot()
		if err != nil {
			return err
		}
		versions := s.filesByPath[file.Path]
		for _, existing := range versions {
			if existing.ID == file.ID || (existing.Version == file.Version && existing.Path == file.Path) {
				return nil
			}
		}
		s.filesByPath[file.Path] = append(versions, file)
	}
	return nil
}

func PermissionRequestFromApproval(approval core.Approval) core.PermissionRequest {
	return core.PermissionRequest{
		ID:          approval.ID,
		SessionID:   approval.SessionID,
		ToolCallID:  approval.ToolCallRef,
		ToolName:    approval.ToolName,
		Description: approval.Description,
		Action:      approval.Action,
		Params:      approval.Params,
		Path:        approval.Path,
	}
}

func (s *State) upsertMessage(message core.Message) {
	for i := range s.messages {
		if s.messages[i].ID == message.ID {
			s.messages[i] = message
			return
		}
	}
	s.messages = append(s.messages, message)
}

func cloneSession(session *core.Session) *core.Session {
	if session == nil {
		return nil
	}
	copy := *session
	return &copy
}

func cloneSessionCapabilities(capabilities *core.SessionCapabilities) *core.SessionCapabilities {
	if capabilities == nil {
		return nil
	}
	copy := *capabilities
	return &copy
}

func cloneRun(run *core.Run) *core.Run {
	if run == nil {
		return nil
	}
	copy := *run
	return &copy
}

func cloneTiming(timing *core.RunTiming) *core.RunTiming {
	if timing == nil {
		return nil
	}
	copy := *timing
	return &copy
}
