package controlplane

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

// rulesDaemon records permission mode changes and rule edits.
type rulesDaemon struct {
	*fakeDaemon
	modes   []core.PermissionMode
	added   []core.PermissionRuleRequest
	deleted []string
}

func newRulesDaemon(t *testing.T) *rulesDaemon {
	d := &rulesDaemon{fakeDaemon: newFakeDaemon(t)}
	d.on("PATCH /v1/sessions/{id}/permissions", func(r *http.Request) any {
		mode := core.PermissionMode(decode[core.UpdateSessionPermissionModeRequest](r).PermissionMode)
		d.modes = append(d.modes, mode)
		return core.SessionResponse{Session: core.Session{ID: r.PathValue("id"), PermissionMode: mode}}
	}).on("GET /v1/sessions/{id}/permission-rules", func(*http.Request) any {
		return core.PermissionRulesResponse{Rules: testRules()}
	}).on("POST /v1/sessions/{id}/permission-rules", func(r *http.Request) any {
		request := decode[core.PermissionRuleRequest](r)
		d.added = append(d.added, request)
		rule := permission.Rule{ID: "rule_new", Tool: request.Tool, Pattern: request.Pattern, Effect: request.Effect, Scope: request.Scope}
		if request.Scope == permission.ScopeSession {
			rule.SessionID = r.PathValue("id")
		}
		return core.PermissionRuleResponse{Rule: rule}
	}).on("DELETE /v1/permission-rules/{id}", func(r *http.Request) any {
		d.deleted = append(d.deleted, r.PathValue("id"))
		return nil
	}).on("POST /v1/sessions/{id}/system-message", func(*http.Request) any {
		return core.MessageResponse{}
	})
	return d
}

func testRules() []permission.Rule {
	return []permission.Rule{
		{ID: "rule_global", Tool: "read", Pattern: "/home/u/.ssh/**", Effect: permission.Deny, Scope: permission.ScopeGlobal},
		{ID: "rule_parent", Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "parent"},
		{ID: "rule_own", Tool: "edit", Pattern: "/work/**", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "s1"},
	}
}

func TestPermissionsListsModesAndRules(t *testing.T) {
	result := newRulesDaemon(t).run("/permissions")

	if result.Picker == nil {
		t.Fatalf("result = %+v", result)
	}
	var rows []string
	for _, item := range result.Picker.Items {
		rows = append(rows, item.Title+" | "+item.Info+" | "+item.Command)
	}
	want := []string{
		"Ask First |  | /permissions default",
		"Edits Only |  | /permissions accept_edits",
		"Full Auto |  | /permissions full_auto",
		"deny read: /home/u/.ssh/** | global | /permissions delete rule_global",
		"allow bash: go test:* | parent session | /permissions delete rule_parent",
		"allow edit: /work/** | this session | /permissions delete rule_own",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rows:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
}

func TestPermissionsDeleteAsksThenDeletes(t *testing.T) {
	daemon := newRulesDaemon(t)

	result := daemon.run("/permissions delete rule_own")
	if result.Confirm == nil || result.Confirm.ConfirmCommand != "/permissions delete rule_own confirm" || len(daemon.deleted) != 0 {
		t.Fatalf("result = %+v deleted = %v", result, daemon.deleted)
	}
	result = daemon.run(result.Confirm.ConfirmCommand)
	if len(daemon.deleted) != 1 || daemon.deleted[0] != "rule_own" || result.Text != "🗑️ Rule deleted: allow edit: /work/**" {
		t.Fatalf("result = %+v deleted = %v", result, daemon.deleted)
	}
}

func TestOnlyTheOwnerChangesGlobalRules(t *testing.T) {
	for _, role := range []core.Role{core.RoleMember, core.RoleOwner} {
		daemon := newRulesDaemon(t)
		owner := role == core.RoleOwner

		deleted := daemon.runAs(role, "/permissions delete rule_global confirm")
		added := daemon.runAs(role, "/permissions add deny read ~/.ssh/** global")

		if owner != (len(daemon.deleted) == 1 && len(daemon.added) == 1) {
			t.Fatalf("%s: deleted = %v added = %+v (%q, %q)", role, daemon.deleted, daemon.added, deleted.Text, added.Text)
		}
		if !owner && (deleted.Text != "Only the owner can change global rules." || added.Text != deleted.Text) {
			t.Fatalf("non-owner replies = %q, %q", deleted.Text, added.Text)
		}
		if owner && daemon.added[0] != (core.PermissionRuleRequest{Tool: "read", Pattern: "~/.ssh/**", Effect: permission.Deny, Scope: permission.ScopeGlobal}) {
			t.Fatalf("added = %+v", daemon.added[0])
		}
	}
}

func TestOnlyTheOwnerSwitchesThePermissionMode(t *testing.T) {
	for _, role := range []core.Role{core.RoleMember, core.RoleOwner} {
		daemon := newRulesDaemon(t)
		owner := role == core.RoleOwner

		result := daemon.runAs(role, "/permissions full_auto")

		if owner != (len(daemon.modes) == 1) {
			t.Fatalf("%s: modes = %v result = %q", role, daemon.modes, result.Text)
		}
		if !owner && result.Text != "Only the owner can switch the permission mode." {
			t.Fatalf("non-owner reply = %q", result.Text)
		}
	}
}

func TestGuestsChangeNoRules(t *testing.T) {
	daemon := newRulesDaemon(t)

	deleted := daemon.runAs(core.RoleGuest, "/permissions delete rule_own confirm")
	added := daemon.runAs(core.RoleGuest, "/permissions add allow bash go test:*")

	if len(daemon.deleted) != 0 || len(daemon.added) != 0 || deleted.Text != "Guests cannot change permission rules." || added.Text != deleted.Text {
		t.Fatalf("deleted = %v added = %+v (%q, %q)", daemon.deleted, daemon.added, deleted.Text, added.Text)
	}
}

func TestPermissionsUsageWarnsThatTestRunnersRunCode(t *testing.T) {
	result := newRulesDaemon(t).run("/permissions nonsense")
	if !strings.Contains(result.Text, "accept_edits") || !strings.Contains(result.Text, "runs code") || !strings.Contains(result.Text, "redirects included") {
		t.Fatalf("usage = %q", result.Text)
	}
}

func TestPermissionsAddReadsEffectToolAndPattern(t *testing.T) {
	for command, want := range map[string]core.PermissionRuleRequest{
		"/permissions add allow bash go test:*": {Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession},
		"/permissions add ask web_fetch":        {Tool: "web_fetch", Effect: permission.Ask, Scope: permission.ScopeSession},
	} {
		daemon := newRulesDaemon(t)

		result := daemon.runAs(core.RoleMember, command)

		if len(daemon.added) != 1 || daemon.added[0] != want {
			t.Errorf("%s: result = %+v added = %+v", command, result, daemon.added)
		}
	}
	for _, command := range []string{"/permissions add", "/permissions add allow", "/permissions add maybe bash ls:*", "/permissions sometimes"} {
		daemon := newRulesDaemon(t)

		if result := daemon.runAs(core.RoleMember, command); result.Text != permissionsUsage || len(daemon.added) != 0 {
			t.Errorf("%s: result = %+v", command, result)
		}
	}
}
