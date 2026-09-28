package controlplane

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

type rulesRuntime struct {
	tokenReportRuntime
	owner   bool
	guest   bool
	modes   []core.PermissionMode
	rules   []permission.Rule
	added   []core.PermissionRuleRequest
	deleted []string
}

func (r *rulesRuntime) UpdateSessionPermissionMode(_ context.Context, sessionID string, mode core.PermissionMode) (core.Session, error) {
	r.modes = append(r.modes, mode)
	return core.Session{ID: sessionID, PermissionMode: mode}, nil
}

func (r *rulesRuntime) SessionPermissionRules(context.Context, string) ([]permission.Rule, error) {
	return r.rules, nil
}

func (r *rulesRuntime) AddPermissionRule(_ context.Context, sessionID string, request core.PermissionRuleRequest) (permission.Rule, error) {
	r.added = append(r.added, request)
	rule := permission.Rule{ID: "rule_new", Tool: request.Tool, Pattern: request.Pattern, Effect: request.Effect, Scope: request.Scope}
	if request.Scope == permission.ScopeSession {
		rule.SessionID = sessionID
	}
	return rule, nil
}

func (r *rulesRuntime) DeletePermissionRule(_ context.Context, ruleID string) error {
	r.deleted = append(r.deleted, ruleID)
	return nil
}

func (r *rulesRuntime) ManagesPermissionMode() bool { return r.owner }

func (r *rulesRuntime) ManagesRules(scope permission.Scope) bool {
	return !r.guest && (scope == permission.ScopeSession || r.owner)
}

func testRules() []permission.Rule {
	return []permission.Rule{
		{ID: "rule_global", Tool: "read", Pattern: "/home/u/.ssh/**", Effect: permission.Deny, Scope: permission.ScopeGlobal},
		{ID: "rule_parent", Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "parent"},
		{ID: "rule_own", Tool: "edit", Pattern: "/work/**", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "s1"},
	}
}

func TestPermissionsListsModesAndRules(t *testing.T) {
	runtime := &rulesRuntime{rules: testRules()}

	result, err := New(runtime, "").Handle(context.Background(), "key", "/permissions")

	if err != nil || result.Picker == nil {
		t.Fatalf("result = %+v err = %v", result, err)
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
	if len(rows) != len(want) {
		t.Fatalf("rows = %q", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}

func TestPermissionsDeleteAsksThenDeletes(t *testing.T) {
	runtime := &rulesRuntime{rules: testRules()}
	dispatcher := New(runtime, "")

	result, err := dispatcher.Handle(context.Background(), "key", "/permissions delete rule_own")
	if err != nil || result.Confirm == nil || result.Confirm.ConfirmCommand != "/permissions delete rule_own confirm" || len(runtime.deleted) != 0 {
		t.Fatalf("result = %+v deleted = %v err = %v", result, runtime.deleted, err)
	}
	result, err = dispatcher.Handle(context.Background(), "key", result.Confirm.ConfirmCommand)
	if err != nil || len(runtime.deleted) != 1 || runtime.deleted[0] != "rule_own" || result.Text != "🗑️ Rule deleted: allow edit: /work/**" {
		t.Fatalf("result = %+v deleted = %v err = %v", result, runtime.deleted, err)
	}
}

func TestOnlyTheOwnerChangesGlobalRules(t *testing.T) {
	for _, owner := range []bool{false, true} {
		runtime := &rulesRuntime{owner: owner, rules: testRules()}
		dispatcher := New(runtime, "")

		deleted, err := dispatcher.Handle(context.Background(), "key", "/permissions delete rule_global confirm")
		if err != nil {
			t.Fatal(err)
		}
		added, err := dispatcher.Handle(context.Background(), "key", "/permissions add deny read ~/.ssh/** global")
		if err != nil {
			t.Fatal(err)
		}

		if owner != (len(runtime.deleted) == 1 && len(runtime.added) == 1) {
			t.Fatalf("owner=%v: deleted = %v added = %+v (%q, %q)", owner, runtime.deleted, runtime.added, deleted.Text, added.Text)
		}
		if !owner && (deleted.Text != "Only the owner can change global rules." || added.Text != deleted.Text) {
			t.Fatalf("non-owner replies = %q, %q", deleted.Text, added.Text)
		}
		if owner && runtime.added[0] != (core.PermissionRuleRequest{Tool: "read", Pattern: "~/.ssh/**", Effect: permission.Deny, Scope: permission.ScopeGlobal}) {
			t.Fatalf("added = %+v", runtime.added[0])
		}
	}
}

func TestOnlyTheOwnerSwitchesThePermissionMode(t *testing.T) {
	for _, owner := range []bool{false, true} {
		runtime := &rulesRuntime{owner: owner}

		result, err := New(runtime, "").Handle(context.Background(), "key", "/permissions full_auto")

		if err != nil || owner != (len(runtime.modes) == 1) {
			t.Fatalf("owner=%v: modes = %v result = %q err = %v", owner, runtime.modes, result.Text, err)
		}
		if !owner && result.Text != "Only the owner can switch the permission mode." {
			t.Fatalf("non-owner reply = %q", result.Text)
		}
	}
}

func TestGuestsChangeNoRules(t *testing.T) {
	runtime := &rulesRuntime{guest: true, rules: testRules()}
	dispatcher := New(runtime, "")

	deleted, err := dispatcher.Handle(context.Background(), "key", "/permissions delete rule_own confirm")
	if err != nil {
		t.Fatal(err)
	}
	added, err := dispatcher.Handle(context.Background(), "key", "/permissions add allow bash go test:*")
	if err != nil {
		t.Fatal(err)
	}

	if len(runtime.deleted) != 0 || len(runtime.added) != 0 || deleted.Text != "Guests cannot change permission rules." || added.Text != deleted.Text {
		t.Fatalf("deleted = %v added = %+v (%q, %q)", runtime.deleted, runtime.added, deleted.Text, added.Text)
	}
}

func TestPermissionsUsageWarnsThatTestRunnersRunCode(t *testing.T) {
	result, err := New(&rulesRuntime{}, "").Handle(context.Background(), "key", "/permissions nonsense")
	if err != nil || !strings.Contains(result.Text, "accept_edits") || !strings.Contains(result.Text, "runs code") || !strings.Contains(result.Text, "web_research") {
		t.Fatalf("usage = %q err = %v", result.Text, err)
	}
}

func TestPermissionsAddReadsEffectToolAndPattern(t *testing.T) {
	for command, want := range map[string]core.PermissionRuleRequest{
		"/permissions add allow bash go test:*": {Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession},
		"/permissions add ask web_fetch":        {Tool: "web_fetch", Effect: permission.Ask, Scope: permission.ScopeSession},
	} {
		runtime := &rulesRuntime{}

		result, err := New(runtime, "").Handle(context.Background(), "key", command)

		if err != nil || len(runtime.added) != 1 || runtime.added[0] != want {
			t.Errorf("%s: result = %+v added = %+v err = %v", command, result, runtime.added, err)
		}
	}
	for _, command := range []string{"/permissions add", "/permissions add allow", "/permissions add maybe bash ls:*", "/permissions sometimes"} {
		runtime := &rulesRuntime{}

		result, err := New(runtime, "").Handle(context.Background(), "key", command)

		if err != nil || result.Text != permissionsUsage || len(runtime.added) != 0 {
			t.Errorf("%s: result = %+v err = %v", command, result, err)
		}
	}
}
