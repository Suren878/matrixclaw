package controlplane

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

// agentSessionsDaemon creates sessions, offers one installed external agent
// and records the runs it is asked to continue.
type agentSessionsDaemon struct {
	*fakeDaemon
	created   []core.CreateSessionRequest
	continued []string
}

func newAgentSessionsDaemon(t *testing.T) *agentSessionsDaemon {
	d := &agentSessionsDaemon{fakeDaemon: newFakeDaemon(t)}
	d.on("POST /v1/sessions", func(r *http.Request) any {
		request := decode[core.CreateSessionRequest](r)
		d.created = append(d.created, request)
		session := core.Session{ID: "s_new", Title: request.Title, RuntimeID: core.SessionRuntime(request.RuntimeID)}
		d.sessions = append(d.sessions, session)
		return core.SessionResponse{Session: session}
	}).on("GET /v1/external-agents", func(*http.Request) any {
		return core.ExternalAgentsResponse{Agents: []core.ExternalAgentDescriptor{{ID: "claude", Installed: true, Enabled: true}}}
	}).on("POST /v1/messages", func(r *http.Request) any {
		input := decode[core.HandleMessageInput](r)
		d.continued = append(d.continued, input.SessionID)
		return core.AcceptRunResult{}
	})
	return d
}

func TestOnlyTheOwnerCreatesExternalAgentSessions(t *testing.T) {
	for _, role := range []core.Role{core.RoleMember, core.RoleOwner} {
		daemon := newAgentSessionsDaemon(t)
		owner := role == core.RoleOwner

		created := daemon.runAs(role, "/session new claude review")
		picker := daemon.runAs(role, "/session new")
		if picker.Picker == nil {
			t.Fatalf("picker = %+v", picker)
		}
		var offered []string
		for _, item := range picker.Picker.Items {
			offered = append(offered, item.ID)
		}
		daemon.runAs(role, "/session new matrixclaw")

		agentSessions := 0
		for _, request := range daemon.created {
			if request.RuntimeID == string(core.SessionRuntimeExternalAgent) {
				agentSessions++
			}
		}
		if owner != (agentSessions == 1) || len(daemon.created) != agentSessions+1 {
			t.Fatalf("%s: created = %+v (%q)", role, daemon.created, created.Text)
		}
		if owner != strings.Contains(strings.Join(offered, " "), "claude") {
			t.Fatalf("%s: picker offers %v", role, offered)
		}
		if !owner && created.Text != "Only the owner can start an external agent session." {
			t.Fatalf("non-owner reply = %q", created.Text)
		}
		if daemon.bound != "s_new" {
			t.Fatalf("%s: the new session is not bound: %q", role, daemon.bound)
		}
	}
}

func TestOnlyTheOwnerReachesSessionsThatRunUnattended(t *testing.T) {
	sessions := []core.Session{
		{ID: "agent", Title: "agent", RuntimeID: core.SessionRuntimeExternalAgent, PermissionMode: core.PermissionModeFullAuto},
		{ID: "auto", Title: "auto", RuntimeID: core.SessionRuntimeMatrixClaw, PermissionMode: core.PermissionModeFullAuto},
		{ID: "plain", Title: "plain", RuntimeID: core.SessionRuntimeMatrixClaw, PermissionMode: core.PermissionModeDefault},
	}
	for _, role := range []core.Role{core.RoleMember, core.RoleOwner} {
		for _, session := range sessions {
			daemon := newAgentSessionsDaemon(t)
			daemon.sessions, daemon.bound = sessions, ""

			used := daemon.runAs(role, "/session use "+session.ID)
			reachable := role == core.RoleOwner || session.ID == "plain"
			if reachable != (daemon.bound == session.ID) {
				t.Fatalf("%s %s: bound = %q (%q)", role, session.ID, daemon.bound, used.Text)
			}
			if !reachable && used.Text != unattendedRefusal {
				t.Fatalf("%s %s: reply = %q", role, session.ID, used.Text)
			}

			daemon.bound = session.ID
			daemon.runAs(role, "/continue")
			if reachable != (len(daemon.continued) == 1) {
				t.Fatalf("%s %s: continued = %v", role, session.ID, daemon.continued)
			}
		}
	}
}
