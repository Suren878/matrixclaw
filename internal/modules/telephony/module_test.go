package telephony

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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

	result, err := call.Execute(context.Background(), tools.Call{Approved: true, Args: json.RawMessage(`{"to":"+1 555 0100","objective":"book"}`), SessionID: "s1"})
	if err != nil || result.IsError {
		t.Fatalf("call = %+v, %v", result, err)
	}
	if string(result.Metadata.(json.RawMessage)) != `{"id":"call_1"}` || body["profile"] != "main" || body["objective"] != "book" {
		t.Fatalf("metadata %v body %v", result.Metadata, body)
	}
	result, err = end.Execute(context.Background(), tools.Call{Client: "telephony", ExternalKey: "call_1"})
	if err != nil || result.IsError {
		t.Fatalf("end = %+v, %v", result, err)
	}
	if len(requests) != 2 || requests[0] != "POST /v1/calls Bearer gw" || requests[1] != "DELETE /v1/calls/call_1 Bearer gw" {
		t.Fatalf("requests = %v", requests)
	}
}

func TestDescriptorProbesTheGateway(t *testing.T) {
	status := `{"status":"ready"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(status))
	}))
	defer server.Close()
	m := applied(t, setup.TelephonyConfig{Enabled: true, GatewayURL: server.URL})
	if module := m.Descriptor(context.Background()); !module.GatewayReachable || module.Status != "Ready" {
		t.Fatalf("module = %+v", module)
	}
	status = `{"status":"not_ready","error":"ARI down"}`
	if module := m.Descriptor(context.Background()); !module.GatewayReachable || module.Status != "Gateway degraded" {
		t.Fatalf("module = %+v", module)
	}
	server.Close()
	if module := m.Descriptor(context.Background()); module.GatewayReachable || module.Status != "Gateway unreachable" {
		t.Fatalf("module = %+v", module)
	}
}
