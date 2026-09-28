package webtools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestWebFetchSubjectIsTheHost(t *testing.T) {
	registry := tools.NewRegistry(NewWebFetchExecutor())
	for args, want := range map[string]permission.Subject{
		`{"url":"https://Docs.Example.com:8443/a?b=1"}`: {Kind: permission.KindDomain, Value: "docs.example.com"},
		`{"url":"https://Bücher.example./"}`:            {Kind: permission.KindDomain, Value: "xn--bcher-kva.example"},
		`{"url":"not a url"}`:                           {},
		`{}`:                                            {},
	} {
		if got := registry.Subject("web_fetch", tools.Call{Args: json.RawMessage(args)}); got != want {
			t.Errorf("%s: subject = %+v, want %+v", args, got, want)
		}
	}
}

func TestWebFetchRedirectsMeetTheRules(t *testing.T) {
	var asked []permission.Subject
	recheck := func(_ context.Context, subject permission.Subject) error {
		asked = append(asked, subject)
		return errors.New("blocked by rule web_fetch: evil.example")
	}
	req, err := http.NewRequestWithContext(withRecheck(context.Background(), recheck), http.MethodGet, "https://Evil.Example./x", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = webFetchClient.CheckRedirect(req, nil)
	if err == nil || !strings.Contains(err.Error(), "blocked by rule") || len(asked) != 1 || asked[0].Value != "evil.example" {
		t.Fatalf("redirect error = %v, asked = %+v", err, asked)
	}
}
