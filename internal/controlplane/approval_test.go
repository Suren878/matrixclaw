package controlplane

import (
	"net/http"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

// approvalDaemon records the approval decisions it is sent.
func approvalDaemon(t *testing.T) (*fakeDaemon, *[]string, *[]core.ApprovalResolveRequest) {
	var ids []string
	var requests []core.ApprovalResolveRequest
	daemon := newFakeDaemon(t).on("POST /v1/approvals/{id}/resolve", func(r *http.Request) any {
		request := decode[core.ApprovalResolveRequest](r)
		ids = append(ids, r.PathValue("id"))
		requests = append(requests, request)
		return core.ApprovalResponse{Approval: core.Approval{ID: r.PathValue("id"), ToolName: "bash", State: core.ApprovalStateRejected, Reason: request.Reason}}
	})
	return daemon, &ids, &requests
}

func TestApprovalDenyCarriesTheReason(t *testing.T) {
	daemon, ids, requests := approvalDaemon(t)

	result := daemon.run(DenyWithReasonPrompt("approval_1").SubmitCommandPrefix + "use  the staging db")

	if len(*ids) != 1 || (*ids)[0] != "approval_1" {
		t.Fatalf("result = %+v ids = %v", result, *ids)
	}
	if (*requests)[0] != (core.ApprovalResolveRequest{Reason: "use  the staging db"}) {
		t.Fatalf("request = %+v", (*requests)[0])
	}
	if result.Text != "❌ Denied bash: use  the staging db" || !result.ReloadSnapshot {
		t.Fatalf("result = %+v", result)
	}
}

func TestApprovalRejectsOtherAnswers(t *testing.T) {
	for _, command := range []string{"/approval", "/approval deny", "/approval allow approval_1"} {
		daemon, ids, _ := approvalDaemon(t)

		result := daemon.run(command)

		if result.Text != approvalUsage || len(*ids) != 0 {
			t.Errorf("%s: result = %+v ids = %v", command, result, *ids)
		}
	}
}

func TestApprovalDenySplitsOnAnyWhitespace(t *testing.T) {
	daemon, ids, requests := approvalDaemon(t)

	daemon.run("/approval deny\tapproval_1\nnot on\tproduction")

	if len(*ids) != 1 || (*ids)[0] != "approval_1" || (*requests)[0].Reason != "not on\tproduction" {
		t.Fatalf("ids = %v requests = %+v", *ids, *requests)
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
