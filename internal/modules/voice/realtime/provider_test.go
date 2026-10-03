package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/coder/websocket"
)

// echoCodec speaks a toy protocol: setup is acknowledged with {"ready":true},
// inputs are sent as {"text":...} and every server message decodes to a
// transcript.
type echoCodec struct{}

func (echoCodec) Setup() ([]any, error) { return []any{map[string]any{"setup": true}}, nil }
func (echoCodec) SetupDone(msg []byte) (bool, error) {
	if strings.Contains(string(msg), "fail") {
		return false, errors.New("rejected")
	}
	return strings.Contains(string(msg), "ready"), nil
}
func (echoCodec) Encode(input ProviderInput) ([]any, error) {
	return []any{map[string]string{"text": input.Text}}, nil
}
func (echoCodec) Decode(msg []byte) []ProviderOutput {
	return []ProviderOutput{{Type: ProviderOutputAssistantTranscript, Text: string(msg)}}
}

func echoServer(t *testing.T, setupReply string) (*httptest.Server, chan http.Header) {
	headers := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := r.Context()
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"other":1}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(setupReply))
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			_ = conn.Write(ctx, websocket.MessageText, data)
		}
	}))
	t.Cleanup(server.Close)
	return server, headers
}

func echoSpec(url string) ProviderSpec {
	return ProviderSpec{
		ID:   "echo",
		Name: "Echo",
		Dial: func(cfg ProviderConfig, model string) (string, http.Header, error) {
			return "ws" + strings.TrimPrefix(url, "http"), http.Header{"Authorization": {"Bearer " + cfg.APIKey}}, nil
		},
		NewCodec: func(ProviderConnectRequest) Codec { return echoCodec{} },
	}
}

func TestConnectRunsTheCodecOverOneWebsocket(t *testing.T) {
	server, headers := echoServer(t, `{"ready":true}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := connect(ctx, echoSpec(server.URL), ProviderConfig{APIKey: "k"}, ProviderConnectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := (<-headers).Get("Authorization"); got != "Bearer k" {
		t.Fatalf("Authorization = %q", got)
	}
	if err := conn.Send(ctx, ProviderInput{Type: ProviderInputTextAppend, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	output, err := conn.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if output.Type != ProviderOutputAssistantTranscript || output.Text != `{"text":"hi"}` {
		t.Fatalf("output = %+v", output)
	}
	if err := conn.Close(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Receive(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("Receive after close = %v, want EOF", err)
	}
}

func TestConnectFailsWhenSetupIsRejected(t *testing.T) {
	server, _ := echoServer(t, `{"fail":true}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := connect(ctx, echoSpec(server.URL), ProviderConfig{APIKey: "k"}, ProviderConnectRequest{}); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("err = %v, want the setup rejection", err)
	}
}

func TestConnectNeedsAKeyAndAListedModel(t *testing.T) {
	spec := echoSpec("http://unused")
	if _, err := connect(context.Background(), spec, ProviderConfig{}, ProviderConnectRequest{}); !errors.Is(err, errAPIKeyRequired) {
		t.Fatalf("err = %v, want key required", err)
	}
	spec.Models = []string{"a"}
	if _, err := connect(context.Background(), spec, ProviderConfig{APIKey: "k"}, ProviderConnectRequest{ModelID: "b"}); err == nil {
		t.Fatal("expected an unlisted model to fail")
	}
}

var langSpec = ProviderSpec{
	ID:           "lang",
	Name:         "Lang",
	Endpoint:     "wss://lang.example/ws",
	DefaultModel: "m1",
	DefaultVoice: "v1",
	KeyEnvs:      []string{"LANG_TEST_KEY"},
	LLMProviders: []string{"openai"},
	Models:       []string{"m1", "m2"},
	Languages:    RegionalLanguages,
}

func TestNormalizeLanguage(t *testing.T) {
	for in, want := range map[string]string{"": "auto", "Auto": "auto", "ru": "ru-RU", "ru_ru": "ru-RU", "EN": "en-US", "en-in": "en-IN", "xx-yy": "xx-YY", "Klingon": "Klingon"} {
		if got := langSpec.NormalizeLanguage(in); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyResolvesKeysInOrder(t *testing.T) {
	m := NewManager(nil, langSpec)
	t.Setenv("LANG_TEST_KEY", "env-key")
	t.Setenv("LANG_CUSTOM_KEY", "custom-key")
	cfg := setup.Config{
		Assistant: setup.AssistantConfig{Name: "Ava"},
		Providers: []setup.ProviderConfig{{ID: "openai", APIKey: "llm-key"}},
		Modules: setup.ModulesConfig{RealtimeVoice: setup.VoiceModuleConfig{Enabled: true, ProviderID: "lang", Providers: map[string]setup.VoiceProviderConfig{
			"lang": {APIKeyEnv: "LANG_CUSTOM_KEY", Language: "ru"},
		}}},
	}
	key := func() string {
		if err := m.Apply(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		return m.currentConfig().Providers["lang"].APIKey
	}
	if got := key(); got != "custom-key" {
		t.Fatalf("key = %q, want the api_key_env value", got)
	}
	cfg.Modules.RealtimeVoice.Providers["lang"] = setup.VoiceProviderConfig{}
	if got := key(); got != "llm-key" {
		t.Fatalf("key = %q, want the LLM provider's", got)
	}
	cfg.Providers = nil
	if got := key(); got != "env-key" {
		t.Fatalf("key = %q, want the provider's env", got)
	}
	got := m.currentConfig()
	if !got.Enabled || !strings.Contains(got.Instructions, `"Ava"`) {
		t.Fatalf("config = %+v", got)
	}
	effective := got.provider(langSpec)
	if effective.Endpoint != langSpec.Endpoint || effective.ModelID != "m1" || effective.VoiceID != "v1" || effective.Language != "auto" {
		t.Fatalf("effective = %+v", effective)
	}
}

func TestEditStoresOnlyUserValues(t *testing.T) {
	m := NewManager(nil, langSpec)
	module := setup.VoiceModuleConfig{Providers: map[string]setup.VoiceProviderConfig{"lang": {APIKey: "old"}}}
	enabled := true
	err := m.Edit(&module, setup.VoiceModuleUpdate{Enabled: &enabled, ProviderID: "LANG", ProviderConfig: &setup.VoiceProviderConfig{
		ModelID: "m1", VoiceID: "v2", Language: "ru", Endpoint: "wss://lang.example/ws", RuntimeMode: "always_running",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := setup.VoiceProviderConfig{APIKey: "old", VoiceID: "v2", Language: "ru-RU"}
	if !module.Enabled || module.ProviderID != "lang" || module.Providers["lang"] != want {
		t.Fatalf("module = %+v", module)
	}
	if err := m.Edit(&module, setup.VoiceModuleUpdate{ProviderID: "nope"}); err == nil {
		t.Fatal("expected an unknown provider to fail")
	}
}

func TestDescriptorReportsReadyFromTheKeyCheck(t *testing.T) {
	spec := langSpec
	checks := 0
	spec.CheckKey = func(_ context.Context, key string) ([]string, error) {
		checks++
		if key == "bad" {
			return nil, &KeyError{Status: http.StatusUnauthorized, Message: "denied"}
		}
		return nil, nil
	}
	m := NewManager(nil, spec)
	m.setConfig(Config{Enabled: true, ProviderID: "lang", Providers: map[string]ProviderConfig{"lang": {APIKey: "good"}}})
	module := m.Descriptor(context.Background())
	if !module.Ready || module.Status != "Ready" || len(module.Providers[0].Languages) == 0 {
		t.Fatalf("module = %+v", module)
	}
	m.Descriptor(context.Background())
	if checks != 1 {
		t.Fatalf("key checked %d times, want a cached result", checks)
	}
	m.setConfig(Config{Enabled: true, ProviderID: "lang", Providers: map[string]ProviderConfig{"lang": {APIKey: "bad"}}})
	if module := m.Descriptor(context.Background()); module.Ready || module.Status != "Invalid API key" {
		t.Fatalf("module = %+v", module)
	}
}

func TestFunctionDeclarationsSanitizeSchemas(t *testing.T) {
	tools := []ToolDeclaration{{Name: "t", Description: "d", Parameters: json.RawMessage(`{"type":"object","$schema":"x","additionalProperties":false,"properties":{"a":{"type":"string","enum":["", "x"]}}}`)}}
	plain := FunctionDeclarations(tools, false, false)[0]
	params := plain["parameters"].(map[string]any)
	if _, ok := params["$schema"]; ok {
		t.Fatal("$schema kept")
	}
	if _, ok := params["additionalProperties"]; ok {
		t.Fatal("additionalProperties kept")
	}
	if _, ok := plain["type"]; ok {
		t.Fatal("untyped declaration has a type")
	}
	enum := params["properties"].(map[string]any)["a"].(map[string]any)["enum"].([]any)
	if len(enum) != 1 {
		t.Fatalf("enum = %v", enum)
	}
	typed := FunctionDeclarations(tools, true, true)[0]
	if typed["type"] != "function" || typed["parameters"].(map[string]any)["additionalProperties"] != false {
		t.Fatalf("typed = %v", typed)
	}
}
