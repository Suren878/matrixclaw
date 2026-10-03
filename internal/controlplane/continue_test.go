package controlplane

import (
	"net/http"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func continueDaemon(t *testing.T) (*fakeDaemon, *[]core.HandleMessageInput) {
	var continued []core.HandleMessageInput
	daemon := newFakeDaemon(t).on("POST /v1/messages", func(r *http.Request) any {
		input := decode[core.HandleMessageInput](r)
		continued = append(continued, input)
		return core.AcceptRunResult{SessionID: input.SessionID}
	})
	return daemon, &continued
}

func TestContinueCommandContinuesTheCurrentSession(t *testing.T) {
	daemon, continued := continueDaemon(t)

	result := daemon.run("/continue")

	if result.Text != "Continuing the last run." || len(*continued) != 1 {
		t.Fatalf("result = %+v continued = %+v", result, *continued)
	}
	if input := (*continued)[0]; !input.Continue || input.SessionID != "s1" || input.ExternalKey != "key" {
		t.Fatalf("continue request = %+v", input)
	}
}
