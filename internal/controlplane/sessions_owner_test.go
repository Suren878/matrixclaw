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
