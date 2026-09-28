package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type approvalRuntime struct {
	ids      []string
	requests []core.ApprovalResolveRequest
}

func (r *approvalRuntime) ResolveApproval(_ context.Context, approvalID string, request core.ApprovalResolveRequest) (core.Approval, error) {
	r.ids = append(r.ids, approvalID)
	r.requests = append(r.requests, request)
	return core.Approval{ID: approvalID, ToolName: "bash", State: core.ApprovalStateRejected, Reason: request.Reason}, nil
}

func TestApprovalDenyCarriesTheReason(t *testing.T) {
	runtime := &approvalRuntime{}

	result, err := New(runtime, "").Handle(context.Background(), "key", DenyWithReasonPrompt("approval_1").SubmitCommandPrefix+"use  the staging db")

	if err != nil || len(runtime.ids) != 1 || runtime.ids[0] != "approval_1" {
		t.Fatalf("result = %+v ids = %v err = %v", result, runtime.ids, err)
	}
	if runtime.requests[0] != (core.ApprovalResolveRequest{Reason: "use  the staging db"}) {
		t.Fatalf("request = %+v", runtime.requests[0])
	}
	if result.Text != "❌ Denied bash: use  the staging db" || !result.ReloadSnapshot {
		t.Fatalf("result = %+v", result)
	}
}

func TestApprovalRejectsOtherAnswers(t *testing.T) {
	for _, command := range []string{"/approval", "/approval deny", "/approval allow approval_1"} {
		runtime := &approvalRuntime{}

		result, err := New(runtime, "").Handle(context.Background(), "key", command)

		if err != nil || result.Text != approvalUsage || len(runtime.ids) != 0 {
			t.Errorf("%s: result = %+v ids = %v err = %v", command, result, runtime.ids, err)
		}
	}
}

func TestApprovalDenySplitsOnAnyWhitespace(t *testing.T) {
	runtime := &approvalRuntime{}

	_, err := New(runtime, "").Handle(context.Background(), "key", "/approval deny\tapproval_1\nnot on\tproduction")

	if err != nil || len(runtime.ids) != 1 || runtime.ids[0] != "approval_1" || runtime.requests[0].Reason != "not on\tproduction" {
		t.Fatalf("ids = %v requests = %+v err = %v", runtime.ids, runtime.requests, err)
	}
}

func TestDeniedApprovalNamesTheApprovalOfADenyCommand(t *testing.T) {
	for command, want := range map[string]string{
		DenyWithReasonPrompt("approval_1").SubmitCommandPrefix: "approval_1",
		"/approval deny approval_2 not now":                    "approval_2",
		"/approval deny":                                       "",
		"/permissions delete rule_1":                           "",
	} {
		if got, ok := DeniedApproval(command); got != want || ok != (want != "") {
			t.Errorf("DeniedApproval(%q) = %q, %v", command, got, ok)
		}
	}
}
