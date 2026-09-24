package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type continueRuntime struct {
	tokenReportRuntime
	continued []string
}

func (r *continueRuntime) ContinueSession(_ context.Context, externalKey string, sessionID string) (core.AcceptRunResult, error) {
	r.continued = append(r.continued, externalKey+"/"+sessionID)
	return core.AcceptRunResult{SessionID: sessionID}, nil
}

func TestContinueCommandContinuesTheCurrentSession(t *testing.T) {
	runtime := &continueRuntime{}

	result, err := New(runtime, "").Handle(context.Background(), "key", "/continue")

	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Continuing the last run." || len(runtime.continued) != 1 || runtime.continued[0] != "key/s1" {
		t.Fatalf("result = %+v continued = %v", result, runtime.continued)
	}
}
