package viewmodel

import (
	surfacehistory "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/history"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/clientruntime"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

type Snapshot struct {
	SessionID             string
	Session               *core.Session
	Context               *core.ContextReport
	Plan                  *core.SessionPlan
	Run                   *core.Run
	Timing                *core.RunTiming
	Messages              []surfacemessage.Message
	ToolUpdates           []core.ToolUpdate
	Approvals             []surfacepermission.PermissionRequest
	ApprovalNotifications []surfacepermission.PermissionNotification
	Files                 []surfacehistory.File
}

func cloneSession(session *core.Session) *core.Session {
	if session == nil {
		return nil
	}
	copy := *session
	return &copy
}

func cloneRun(run *core.Run) *core.Run {
	if run == nil {
		return nil
	}
	copy := *run
	return &copy
}

func FromStateSnapshot(snapshot clientruntime.StateSnapshot) Snapshot {
	out := Snapshot{
		SessionID:   snapshot.SessionID,
		Session:     cloneSession(snapshot.Session),
		Context:     cloneContextReport(snapshot.Context),
		Plan:        cloneSessionPlan(snapshot.Plan),
		Run:         cloneRun(snapshot.Run),
		Timing:      cloneTiming(snapshot.Timing),
		Messages:    ToSurfaceMessages(snapshot.Messages),
		ToolUpdates: append([]core.ToolUpdate(nil), snapshot.ToolUpdates...),
		Files:       ToSurfaceFiles(snapshot.Files),
	}
	for _, approval := range snapshot.Approvals {
		out.Approvals = append(out.Approvals, ToSurfacePermissionRequest(approval))
	}
	for _, notification := range snapshot.ApprovalNotifications {
		out.ApprovalNotifications = append(out.ApprovalNotifications, ToSurfacePermissionNotification(notification))
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

func cloneTiming(timing *core.RunTiming) *core.RunTiming {
	if timing == nil {
		return nil
	}
	copy := *timing
	return &copy
}

type ReadModel struct {
	state *clientruntime.State
}

func NewReadModel(snapshot core.ClientSnapshot) *ReadModel {
	return &ReadModel{state: clientruntime.NewState(snapshot)}
}

func (m *ReadModel) Snapshot() Snapshot {
	return FromStateSnapshot(m.state.Snapshot())
}

func (m *ReadModel) Apply(event daemonclient.LiveEvent) error {
	return m.state.Apply(event)
}
