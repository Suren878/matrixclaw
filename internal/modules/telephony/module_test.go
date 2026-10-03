package telephony

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func applied(t *testing.T, telephony setup.TelephonyConfig) *Module {
	t.Helper()
	m := New()
	if err := m.Apply(context.Background(), setup.Config{Modules: setup.ModulesConfig{Telephony: telephony}}); err != nil {
		t.Fatal(err)
	}
	return m
}

func toolIDs(m *Module) []string {
	ids := []string{}
	for _, tool := range m.Tools() {
		ids = append(ids, tool.Spec().ID)
	}
	return ids
}

func TestToolsOnlyWhileTelephonyIsConfigured(t *testing.T) {
	if ids := toolIDs(applied(t, setup.TelephonyConfig{GatewayURL: "http://gw"})); len(ids) != 0 {
		t.Fatalf("disabled telephony offers %v", ids)
	}
	if ids := toolIDs(applied(t, setup.TelephonyConfig{Enabled: true})); len(ids) != 0 {
		t.Fatalf("telephony without a gateway offers %v", ids)
	}
	ids := toolIDs(applied(t, setup.TelephonyConfig{Enabled: true, GatewayURL: "http://gw"}))
	if len(ids) != 2 || ids[0] != CallToolID || ids[1] != EndCallToolID {
		t.Fatalf("tools = %v", ids)
	}
}

func TestToolsTalkToTheGateway(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(`{"call":{"id":"call_1"}}`))
		}
	}))
	defer server.Close()
	m := applied(t, setup.TelephonyConfig{Enabled: true, GatewayURL: server.URL, GatewayToken: "gw", DefaultProfile: "main"})
	call, end := m.Tools()[0], m.Tools()[1]

	result, err := call.Execute(context.Background(), tools.Call{Args: json.RawMessage(`{"to":"+1 555 0100","objective":"book"}`), SessionID: "s1"})
	if err != nil || result.IsError() {
		t.Fatalf("call = %+v, %v", result, err)
	}
	if string(result.Metadata.(json.RawMessage)) != `{"id":"call_1"}` || body["profile"] != "main" || body["objective"] != "book" {
		t.Fatalf("metadata %v body %v", result.Metadata, body)
	}
	result, err = end.Execute(context.Background(), tools.Call{Client: "telephony", ExternalKey: "call_1"})
	if err != nil || result.IsError() {
		t.Fatalf("end = %+v, %v", result, err)
	}
	if len(requests) != 2 || requests[0] != "POST /v1/calls Bearer gw" || requests[1] != "DELETE /v1/calls/call_1 Bearer gw" {
		t.Fatalf("requests = %v", requests)
	}
}

func TestSettingsProbeTheGateway(t *testing.T) {
	status := `{"ready":true}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(status))
	}))
	defer server.Close()
	m := applied(t, setup.TelephonyConfig{Enabled: true, GatewayURL: server.URL})
	state := func() string {
		item, _ := modules.Find(m.Settings(context.Background()), []string{"status"})
		return item.Display
	}
	if got := state(); got != "Ready" {
		t.Fatalf("state = %q", got)
	}
	status = `{"ready":false,"error":"ARI down"}`
	if got := state(); got != "Gateway degraded" {
		t.Fatalf("state = %q", got)
	}
	server.Close()
	if got := state(); got != "Gateway unreachable" {
		t.Fatalf("state = %q", got)
	}
}

func TestSettingsChangeTheGatewayAndRefuseABadURL(t *testing.T) {
	m := applied(t, setup.TelephonyConfig{})
	cfg := setup.Config{}
	for _, change := range []struct{ path, value string }{{"gateway_url", "http://gw:8090"}, {"gateway_token", "secret"}, {"enabled", "on"}} {
		edit, err := m.Change(context.Background(), []string{change.path}, change.value)
		if err != nil {
			t.Fatal(err)
		}
		if err := edit.Config(&cfg); err != nil {
			t.Fatal(change.path, err)
		}
	}
	if telephony := cfg.Modules.Telephony; !telephony.Enabled || telephony.GatewayURL != "http://gw:8090" || telephony.GatewayToken != "secret" {
		t.Fatalf("telephony = %+v", telephony)
	}
	edit, _ := m.Change(context.Background(), []string{"gateway_url"}, "gw")
	if err := edit.Config(&cfg); !errors.Is(err, modules.ErrInvalidSetting) {
		t.Fatalf("bad URL error = %v", err)
	}
	if _, err := m.Change(context.Background(), []string{"nope"}, ""); !errors.Is(err, modules.ErrUnknownSetting) {
		t.Fatalf("unknown setting error = %v", err)
	}
}
