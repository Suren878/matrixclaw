package gemini

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

func TestSetupMessage(t *testing.T) {
	c := Spec.NewCodec(realtime.ProviderConnectRequest{
		ModelID: "models/gemini-3.1-flash-live-preview", VoiceID: "Kore", Language: "ru-RU", SystemInstruction: "Be brief.",
		Tools: []realtime.ToolDeclaration{{Name: "telephony_end_call", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	messages, err := c.Setup()
	if err != nil || len(messages) != 1 {
		t.Fatalf("setup = %v, %v", messages, err)
	}
	got := encodeJSON(t, messages[0])
	for _, want := range []string{`"model":"models/gemini-3.1-flash-live-preview"`, `"voiceName":"Kore"`, `"languageCode":"ru-RU"`, `"thinkingLevel":"low"`, `"text":"Be brief."`, `"functionDeclarations":[{`} {
		if !strings.Contains(got, want) {
			t.Errorf("setup lacks %s: %s", want, got)
		}
	}
	native := Spec.NewCodec(realtime.ProviderConnectRequest{ModelID: "gemini-2.5-flash-native-audio-preview-12-2025", Language: "ru-RU"})
	messages, _ = native.Setup()
	if got := encodeJSON(t, messages[0]); strings.Contains(got, "languageCode") || !strings.Contains(got, `"thinkingBudget":0`) {
		t.Fatalf("native audio setup = %s", got)
	}
}

func TestSetupDoneWantsSetupComplete(t *testing.T) {
	c := Spec.NewCodec(realtime.ProviderConnectRequest{})
	if done, err := c.SetupDone([]byte(`{"setupComplete":{}}`)); !done || err != nil {
		t.Fatalf("done = %v, %v", done, err)
	}
	if _, err := c.SetupDone([]byte(`{"serverContent":{}}`)); err == nil {
		t.Fatal("expected an error for another message")
	}
}

func TestEncodeAndDecode(t *testing.T) {
	c := Spec.NewCodec(realtime.ProviderConnectRequest{})
	messages, _ := c.Encode(realtime.ProviderInput{Type: realtime.ProviderInputAudioAppend, AudioBase64: "AAAA"})
	if got := encodeJSON(t, messages); got != `[{"realtimeInput":{"audio":{"data":"AAAA","mimeType":"audio/pcm;rate=16000"}}}]` {
		t.Fatalf("audio = %s", got)
	}
	messages, _ = c.Encode(realtime.ProviderInput{Type: realtime.ProviderInputToolResult, ToolResponses: []realtime.ProviderToolResponse{{ID: "1", Name: "t", Response: map[string]any{"ok": true}}}})
	if got := encodeJSON(t, messages); !strings.Contains(got, `"functionResponses":[{"id":"1","name":"t","response":{"ok":true}}]`) {
		t.Fatalf("tool result = %s", got)
	}
	outputs := c.Decode([]byte(`{"serverContent":{"inputTranscription":{"text":"hi"},"modelTurn":{"parts":[{"inlineData":{"mimeType":"audio/pcm","data":"BBBB"}}]},"turnComplete":true}}`))
	if len(outputs) != 3 || outputs[0].Type != realtime.ProviderOutputInputTranscript || outputs[1].AudioBase64 != "BBBB" || outputs[2].Type != realtime.ProviderOutputTurnComplete {
		t.Fatalf("outputs = %+v", outputs)
	}
}

func TestDialPutsTheKeyInTheURL(t *testing.T) {
	url, header, err := Spec.Dial(realtime.ProviderConfig{APIKey: "k", Endpoint: Spec.Endpoint}, "m")
	if err != nil || !strings.HasSuffix(url, "?key=k") || header != nil {
		t.Fatalf("dial = %q %v %v", url, header, err)
	}
}

func TestLiveModel(t *testing.T) {
	if !liveModel("anything", []string{"bidiGenerateContent"}) || !liveModel("gemini-3.1-flash-live-preview", nil) || liveModel("gemini-live-translate", []string{"bidiGenerateContent"}) || liveModel("gemini-pro", []string{"generateContent"}) {
		t.Fatal("liveModel misclassifies")
	}
}
