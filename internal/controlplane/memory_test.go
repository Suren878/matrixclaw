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
