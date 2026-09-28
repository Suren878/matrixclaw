package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func TestPermissionRulesOfSessionsAndGlobal(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	for _, id := range []string{"parent", "child", "other"} {
		createTestSession(t, st, id)
	}
	for i, rule := range []permission.Rule{
		{ID: "r-global", Tool: "read", Pattern: "/home/u/.ssh/**", Effect: permission.Deny, Scope: permission.ScopeGlobal},
		{ID: "r-parent", Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "parent"},
		{ID: "r-child", Tool: "edit", Pattern: "/work/**", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "child"},
		{ID: "r-other", Tool: "bash", Pattern: "rm:*", Effect: permission.Deny, Scope: permission.ScopeSession, SessionID: "other"},
	} {
		rule.CreatedAt = testEpoch.Add(time.Duration(i) * time.Second)
		if err := st.CreatePermissionRule(ctx, rule); err != nil {
			t.Fatal(err)
		}
	}

	rules, err := st.ListPermissionRules(ctx, []string{"child", "parent"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, rule := range rules {
		ids = append(ids, rule.ID)
	}
	if got := len(ids); got != 3 || ids[0] != "r-global" || ids[1] != "r-parent" || ids[2] != "r-child" {
		t.Fatalf("rules = %v", ids)
	}
	if rules[0].SessionID != "" || rules[1].SessionID != "parent" || rules[1].Pattern != "go test:*" || rules[1].Effect != permission.Allow {
		t.Fatalf("stored rules = %+v", rules)
	}

	if err := st.DeleteSession(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePermissionRule(ctx, "r-global"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePermissionRule(ctx, "r-global"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("second delete error = %v, want ErrNotFound", err)
	}
	rules, err = st.ListPermissionRules(ctx, []string{"child", "parent"})
	if err != nil || len(rules) != 1 || rules[0].ID != "r-child" {
		t.Fatalf("rules after deletes = %+v err = %v", rules, err)
	}
}
