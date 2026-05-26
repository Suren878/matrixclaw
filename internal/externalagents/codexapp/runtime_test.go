package codexapp

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func TestTurnStartParamsIncludesSessionModel(t *testing.T) {
	params := turnStartParams(externalagents.ExternalSession{
		ExternalThreadID: "thread-1",
		Model:            " gpt-5.4-mini ",
		ApprovalPolicy:   "on-request",
	}, "hello")

	if params.ThreadID != "thread-1" {
		t.Fatalf("thread id = %q, want thread-1", params.ThreadID)
	}
	if params.Model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", params.Model)
	}
	if params.ApprovalPolicy != "on-request" {
		t.Fatalf("approval policy = %q, want on-request", params.ApprovalPolicy)
	}
	if len(params.Input) != 1 || params.Input[0].Text != "hello" {
		t.Fatalf("input = %#v, want hello text input", params.Input)
	}
}
