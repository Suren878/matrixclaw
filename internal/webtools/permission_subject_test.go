package webtools

import (
	"encoding/json"
	"testing"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestWebFetchSubjectIsTheHost(t *testing.T) {
	registry := tools.NewRegistry(NewWebFetchExecutor())
	for args, want := range map[string]permission.Subject{
		`{"url":"https://Docs.Example.com:8443/a?b=1"}`: {Kind: permission.KindDomain, Value: "docs.example.com"},
		`{"url":"not a url"}`:                           {},
		`{}`:                                            {},
	} {
		if got := registry.Subject("web_fetch", tools.Call{Args: json.RawMessage(args)}); got != want {
			t.Errorf("%s: subject = %+v, want %+v", args, got, want)
		}
	}
}
