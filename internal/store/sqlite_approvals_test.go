package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func TestApprovalKeepsTheDenialReason(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	approval := core.Approval{ID: "a1", SessionID: "s1", RunID: "r1", ToolCallRef: "c1", ToolName: "bash", State: core.ApprovalStatePending, RequestedAt: testEpoch}
	if err := st.CreateApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}
	decided := testEpoch.Add(1)
	approval.State, approval.Reason, approval.DecidedAt = core.ApprovalStateRejected, "use the staging database", &decided
	if err := st.DecideApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetApproval(ctx, "a1")
	if err != nil || stored.State != core.ApprovalStateRejected || stored.Reason != "use the staging database" {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	listed, err := st.ListApprovals(ctx, "s1", core.ApprovalStateRejected)
	if err != nil || len(listed) != 1 || listed[0].Reason != "use the staging database" {
		t.Fatalf("listed = %+v err = %v", listed, err)
	}
}

func TestAnApprovalIsDecidedOnce(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	approval := core.Approval{ID: "a1", SessionID: "s1", ToolName: "bash", State: core.ApprovalStatePending, RequestedAt: testEpoch}
	if err := st.CreateApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}
	decided := testEpoch.Add(1)
	approval.State, approval.DecidedAt = core.ApprovalStateApproved, &decided
	if err := st.DecideApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}
	approval.State = core.ApprovalStateRejected
	if err := st.DecideApproval(ctx, approval); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("second decision err = %v", err)
	}
	if stored, err := st.GetApproval(ctx, "a1"); err != nil || stored.State != core.ApprovalStateApproved {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
}

func TestApprovalKeepsItsSuggestedRule(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	suggestion := &permission.Suggestion{Tool: "bash", Pattern: "go test:*"}
	if err := st.CreateApproval(ctx, core.Approval{ID: "a1", SessionID: "s1", ToolName: "bash", State: core.ApprovalStatePending, Suggestion: suggestion, RequestedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateApproval(ctx, core.Approval{ID: "a2", SessionID: "s1", ToolName: "bash", State: core.ApprovalStatePending, RequestedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetApproval(ctx, "a1")
	if err != nil || stored.Suggestion == nil || *stored.Suggestion != *suggestion {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	listed, err := st.ListApprovals(ctx, "s1", core.ApprovalStatePending)
	if err != nil || len(listed) != 2 {
		t.Fatalf("listed = %+v err = %v", listed, err)
	}
	for _, approval := range listed {
		if (approval.ID == "a1") != (approval.Suggestion != nil) {
			t.Fatalf("listed %s suggestion = %+v", approval.ID, approval.Suggestion)
		}
	}
}

func TestRunApprovalsAreTheRunsOwnNewestFirst(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	for i, approval := range []core.Approval{
		{ID: "a1", SessionID: "s1", RunID: "r1", State: core.ApprovalStateApproved},
		{ID: "a2", SessionID: "s1", RunID: "r2", State: core.ApprovalStatePending},
		{ID: "a3", SessionID: "s1", RunID: "r1", State: core.ApprovalStatePending},
	} {
		approval.RequestedAt = testEpoch.Add(time.Duration(i) * time.Second)
		if err := st.CreateApproval(ctx, approval); err != nil {
			t.Fatal(err)
		}
	}

	listed, err := st.ListRunApprovals(ctx, "s1", "r1")
	if err != nil || len(listed) != 2 || listed[0].ID != "a3" || listed[1].ID != "a1" {
		t.Fatalf("listed = %+v err = %v", listed, err)
	}
}
