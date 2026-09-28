package store_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
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
	if err := st.UpdateApproval(ctx, approval); err != nil {
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
