package controlplane

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestMemoryCommandListsDaemonMemories(t *testing.T) {
	daemon := newFakeDaemon(t).on("GET /v1/memory", func(*http.Request) any {
		return core.MemoryResponse{Memories: []core.MemoryEntry{{Scope: core.MemoryScopeGlobal, Key: "lang", Content: "Go"}}}
	})

	if result := daemon.run("/memory"); !isScreen[InfoData](result) || !strings.Contains(screenOf[InfoData](t, result).Text, "global/lang: Go") {
		t.Fatalf("result = %+v, want memory info with the daemon entry", result)
	}
}

func TestMemoryPendingApprovesASuggestionForTheOwnerOnly(t *testing.T) {
	daemon := newFakeDaemon(t).
		on("GET /v1/memory", func(r *http.Request) any {
			if r.URL.Query().Get("pending") != "true" {
				t.Errorf("listed %s, want the suggestions", r.URL.RawQuery)
			}
			return core.MemoryResponse{Memories: []core.MemoryEntry{{ID: "m1", Scope: core.MemoryScopeUser, Content: "Prefers pnpm.", Pending: true}}}
		}).
		on("POST /v1/memory/{id}/approve", func(*http.Request) any { return nil })

	if result := daemon.runAs(core.RoleMember, "/memory pending"); isScreen[PickerData](result) {
		t.Fatalf("a member sees suggestions: %+v", result)
	}
	if ids := pickerItemIDs(daemon.run("/memory pending")); len(ids) != 1 || ids[0] != "m1" {
		t.Fatalf("suggestions = %v", ids)
	}
	if ids := pickerItemIDs(daemon.run("/memory pending show m1")); len(ids) != 2 || ids[0] != "approve" || ids[1] != "reject" {
		t.Fatalf("suggestion actions = %v", ids)
	}
	daemon.run("/memory pending approve m1")
	if daemon.called("POST /v1/memory/m1/approve") != 1 {
		t.Fatal("the suggestion was not approved")
	}
}
