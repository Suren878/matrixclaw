package controlplane

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

// agentSessionsRuntime creates sessions and offers one installed external agent.
type agentSessionsRuntime struct {
	rulesRuntime
	created []core.CreateSessionRequest
}

func (r *agentSessionsRuntime) CreateSessionWithOptions(_ context.Context, _ string, request core.CreateSessionRequest) (core.Session, error) {
	r.created = append(r.created, request)
	return core.Session{ID: "s_new", Title: request.Title, RuntimeID: core.SessionRuntime(request.RuntimeID)}, nil
}

func (r *agentSessionsRuntime) ListExternalAgents(context.Context) ([]core.ExternalAgentDescriptor, error) {
	return []core.ExternalAgentDescriptor{{ID: "claude", Installed: true, Enabled: true}}, nil
}

func (r *agentSessionsRuntime) UpdateExternalAgent(context.Context, string, core.UpdateExternalAgentRequest) ([]core.ExternalAgentDescriptor, error) {
	return nil, nil
}

func TestOnlyTheOwnerCreatesExternalAgentSessions(t *testing.T) {
	for _, owner := range []bool{false, true} {
		runtime := &agentSessionsRuntime{rulesRuntime: rulesRuntime{owner: owner}}
		dispatcher := New(runtime, "")

		created, err := dispatcher.Handle(context.Background(), "key", "/session new claude review")
		if err != nil {
			t.Fatal(err)
		}
		picker, err := dispatcher.Handle(context.Background(), "key", "/session new")
		if err != nil || picker.Picker == nil {
			t.Fatalf("picker = %+v err = %v", picker, err)
		}
		var offered []string
		for _, item := range picker.Picker.Items {
			offered = append(offered, item.ID)
		}
		if _, err := dispatcher.Handle(context.Background(), "key", "/session new matrixclaw"); err != nil {
			t.Fatal(err)
		}

		agentSessions := 0
		for _, request := range runtime.created {
			if request.RuntimeID == string(core.SessionRuntimeExternalAgent) {
				agentSessions++
			}
		}
		if owner != (agentSessions == 1) || len(runtime.created) != agentSessions+1 {
			t.Fatalf("owner=%v: created = %+v (%q)", owner, runtime.created, created.Text)
		}
		if owner != strings.Contains(strings.Join(offered, " "), "claude") {
			t.Fatalf("owner=%v: picker offers %v", owner, offered)
		}
		if !owner && created.Text != "Only the owner can start an external agent session." {
			t.Fatalf("non-owner reply = %q", created.Text)
		}
	}
}

// boundSessionsRuntime lists sessions and records the bindings and runs asked for.
type boundSessionsRuntime struct {
	agentSessionsRuntime
	sessions  []core.Session
	bound     string
	continued []string
}

func (r *boundSessionsRuntime) ListSessions(context.Context) ([]core.Session, error) {
	return r.sessions, nil
}

func (r *boundSessionsRuntime) UseSession(_ context.Context, _ string, sessionID string) (core.ClientBinding, error) {
	r.bound = sessionID
	return core.ClientBinding{SessionID: sessionID}, nil
}

func (r *boundSessionsRuntime) CurrentBinding(context.Context, string) (core.ClientBinding, error) {
	return core.ClientBinding{SessionID: r.bound}, nil
}

func (r *boundSessionsRuntime) ContinueSession(_ context.Context, _ string, sessionID string) (core.AcceptRunResult, error) {
	r.continued = append(r.continued, sessionID)
	return core.AcceptRunResult{}, nil
}

func TestOnlyTheOwnerReachesSessionsThatRunUnattended(t *testing.T) {
	sessions := []core.Session{
		{ID: "agent", Title: "agent", RuntimeID: core.SessionRuntimeExternalAgent, PermissionMode: core.PermissionModeFullAuto},
		{ID: "auto", Title: "auto", RuntimeID: core.SessionRuntimeMatrixClaw, PermissionMode: core.PermissionModeFullAuto},
		{ID: "plain", Title: "plain", RuntimeID: core.SessionRuntimeMatrixClaw, PermissionMode: core.PermissionModeDefault},
	}
	for _, owner := range []bool{false, true} {
		for _, session := range sessions {
			runtime := &boundSessionsRuntime{agentSessionsRuntime: agentSessionsRuntime{rulesRuntime: rulesRuntime{owner: owner}}, sessions: sessions}
			dispatcher := New(runtime, "")

			used, err := dispatcher.Handle(context.Background(), "key", "/session use "+session.ID)
			if err != nil {
				t.Fatal(err)
			}
			reachable := owner || session.ID == "plain"
			if reachable != (runtime.bound == session.ID) {
				t.Fatalf("owner=%v %s: bound = %q (%q)", owner, session.ID, runtime.bound, used.Text)
			}
			if !reachable && used.Text != "Only the owner can use a session that runs tools without asking (external agent or full_auto)." {
				t.Fatalf("owner=%v %s: reply = %q", owner, session.ID, used.Text)
			}

			runtime.bound = session.ID
			if _, err := dispatcher.Handle(context.Background(), "key", "/continue"); err != nil {
				t.Fatal(err)
			}
			if reachable != (len(runtime.continued) == 1) {
				t.Fatalf("owner=%v %s: continued = %v", owner, session.ID, runtime.continued)
			}
		}
	}
}
