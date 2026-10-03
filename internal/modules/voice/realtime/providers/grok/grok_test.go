package grok

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

func encodeJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSessionUpdate(t *testing.T) {
	c := Spec.NewCodec(realtime.ProviderConnectRequest{VoiceID: "ara", Language: Spec.NormalizeLanguage("ru-RU"), SystemInstruction: "Be brief.",
		Tools: []realtime.ToolDeclaration{{Name: "t", Parameters: json.RawMessage(`{"type":"object"}`)}}})
	messages, err := c.Setup()
	if err != nil {
		t.Fatal(err)
	}
	got := encodeJSON(t, messages[0])
	for _, want := range []string{`"type":"session.update"`, `"voice":"ara"`, `"language_hint":"ru"`, `"instructions":"Be brief."`, `"type":"function"`, `"rate":16000`} {
		if !strings.Contains(got, want) {
			t.Errorf("session update lacks %s: %s", want, got)
		}
	}
	auto := Spec.NewCodec(realtime.ProviderConnectRequest{Language: "auto"})
	messages, _ = auto.Setup()
	if strings.Contains(encodeJSON(t, messages[0]), "language_hint") {
		t.Fatal("auto language sent a hint")
	}
}

func TestSetupDone(t *testing.T) {
	c := Spec.NewCodec(realtime.ProviderConnectRequest{})
	if done, err := c.SetupDone([]byte(`{"type":"conversation.created"}`)); done || err != nil {
		t.Fatalf("other message: %v %v", done, err)
	}
	if done, err := c.SetupDone([]byte(`{"type":"session.updated"}`)); !done || err != nil {
		t.Fatalf("session.updated: %v %v", done, err)
	}
	if _, err := c.SetupDone([]byte(`{"type":"error","error":{"message":"bad voice"}}`)); err == nil || err.Error() != "bad voice" {
		t.Fatalf("error = %v", err)
	}
}

func TestEncodeAndDecode(t *testing.T) {
	c := Spec.NewCodec(realtime.ProviderConnectRequest{})
	messages, _ := c.Encode(realtime.ProviderInput{Type: realtime.ProviderInputTextAppend, Text: "hi", EndOfTurn: true})
	if got := encodeJSON(t, messages); !strings.Contains(got, `"input_text"`) || !strings.HasSuffix(got, `{"type":"response.create"}]`) {
		t.Fatalf("text = %s", got)
	}
	outputs := c.Decode([]byte(`{"type":"response.function_call_arguments.done","name":"t","call_id":"c1"}`))
	if len(outputs) != 1 || outputs[0].ToolCalls[0].ID != "c1" || string(outputs[0].ToolCalls[0].Args) != "{}" {
		t.Fatalf("outputs = %+v", outputs)
	}
}

func TestLanguageAliases(t *testing.T) {
	for in, want := range map[string]string{"en-us": "en", "pt": "pt-BR", "es": "es-MX", "ru-RU": "ru", "": "auto"} {
		if got := Spec.NormalizeLanguage(in); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}
