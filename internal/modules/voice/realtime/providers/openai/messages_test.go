package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

func TestSessionUpdateMessageUsesGARealtimeShape(t *testing.T) {
	message := sessionUpdateMessage(
		"gpt-realtime-2.1",
		"marin",
		"ru-RU",
		"Speak clearly.",
		[]realtime.ToolDeclaration{{
			Name:        "weather",
			Description: "Read the weather.",
			Parameters: json.RawMessage(`{
				"type":"object",
				"properties":{"city":{"type":"string"}},
				"required":["city"],
				"$schema":"https://json-schema.org/draft/2020-12/schema"
			}`),
		}},
	)

	if got := message["type"]; got != "session.update" {
		t.Fatalf("type = %v, want session.update", got)
	}
	session := requireMap(t, message["session"])
	if got := session["type"]; got != "realtime" {
		t.Fatalf("session.type = %v, want realtime", got)
	}
	if got := session["instructions"]; got != "Speak clearly." {
		t.Fatalf("instructions = %v", got)
	}
	if got := session["parallel_tool_calls"]; got != false {
		t.Fatalf("parallel_tool_calls = %v, want false", got)
	}
	reasoning := requireMap(t, session["reasoning"])
	if got := reasoning["effort"]; got != "low" {
		t.Fatalf("reasoning.effort = %v, want low", got)
	}

	audio := requireMap(t, session["audio"])
	input := requireMap(t, audio["input"])
	inputFormat := requireMap(t, input["format"])
	if got := inputFormat["type"]; got != "audio/pcm" {
		t.Fatalf("input format type = %v", got)
	}
	if got := inputFormat["rate"]; got != 24000 {
		t.Fatalf("input format rate = %v, want 24000", got)
	}
	transcription := requireMap(t, input["transcription"])
	if got := transcription["model"]; got != "gpt-live-transcribe" {
		t.Fatalf("transcription model = %v", got)
	}
	languages, ok := transcription["languages"].([]string)
	if !ok || len(languages) != 1 || languages[0] != "ru" {
		t.Fatalf("transcription languages = %#v, want [ru]", transcription["languages"])
	}
	turnDetection := requireMap(t, input["turn_detection"])
	if got := turnDetection["interrupt_response"]; got != true {
		t.Fatalf("interrupt_response = %v, want true", got)
	}

	output := requireMap(t, audio["output"])
	outputFormat := requireMap(t, output["format"])
	if got := outputFormat["rate"]; got != 24000 {
		t.Fatalf("output format rate = %v, want 24000", got)
	}
	if got := output["voice"]; got != "marin" {
		t.Fatalf("output voice = %v, want marin", got)
	}

	tools, ok := session["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", session["tools"])
	}
	parameters := requireMap(t, tools[0]["parameters"])
	if _, ok := parameters["$schema"]; ok {
		t.Fatal("unsupported $schema field was not removed")
	}
}

func TestMessageDecoderWaitsForFinalInputTranscript(t *testing.T) {
	decoder := newMessageDecoder()
	if outputs := decoder.Decode([]byte(`{
		"type":"input_audio_buffer.committed",
		"item_id":"item_1"
	}`)); len(outputs) != 0 {
		t.Fatalf("commit outputs = %#v, want none", outputs)
	}
	if outputs := decoder.Decode([]byte(`{
		"type":"response.output_audio_transcript.done",
		"transcript":"Здравствуйте!"
	}`)); len(outputs) != 1 || outputs[0].Type != realtime.ProviderOutputAssistantTranscript {
		t.Fatalf("assistant transcript outputs = %#v", outputs)
	}
	if outputs := decoder.Decode([]byte(`{
		"type":"response.done",
		"response":{"status":"completed","output":[]}
	}`)); len(outputs) != 0 {
		t.Fatalf("response.done outputs = %#v, want deferred", outputs)
	}

	outputs := decoder.Decode([]byte(`{
		"type":"conversation.item.input_audio_transcription.completed",
		"item_id":"item_1",
		"transcript":"Привет"
	}`))
	if len(outputs) != 2 {
		t.Fatalf("completed outputs = %#v, want transcript and turn complete", outputs)
	}
	if outputs[0].Type != realtime.ProviderOutputInputTranscript || outputs[0].Text != "Привет" {
		t.Fatalf("first output = %#v", outputs[0])
	}
	if outputs[1].Type != realtime.ProviderOutputTurnComplete {
		t.Fatalf("second output = %#v", outputs[1])
	}
}

func TestMessageDecoderFunctionCallFromCompletedResponse(t *testing.T) {
	decoder := newMessageDecoder()
	outputs := decoder.Decode([]byte(`{
		"type":"response.done",
		"response":{
			"status":"completed",
			"output":[{
				"type":"function_call",
				"call_id":"call_1",
				"name":"weather",
				"arguments":"{\"city\":\"Moscow\"}"
			}]
		}
	}`))
	if len(outputs) != 1 || outputs[0].Type != realtime.ProviderOutputToolCall {
		t.Fatalf("outputs = %#v", outputs)
	}
	if len(outputs[0].ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", outputs[0].ToolCalls)
	}
	call := outputs[0].ToolCalls[0]
	if call.ID != "call_1" || call.Name != "weather" || string(call.Args) != `{"city":"Moscow"}` {
		t.Fatalf("call = %#v", call)
	}
}

func TestMessageDecoderAudioAndErrorEvents(t *testing.T) {
	decoder := newMessageDecoder()
	outputs := decoder.Decode([]byte(`{"type":"response.output_audio.delta","delta":"AQID"}`))
	if len(outputs) != 1 || outputs[0].Type != realtime.ProviderOutputAssistantAudio {
		t.Fatalf("audio outputs = %#v", outputs)
	}
	if outputs[0].MIMEType != "audio/pcm;rate=24000" {
		t.Fatalf("audio MIME type = %q", outputs[0].MIMEType)
	}

	outputs = decoder.Decode([]byte(`{"type":"error","error":{"code":"bad_request","message":"bad audio"}}`))
	if len(outputs) != 1 || outputs[0].Type != realtime.ProviderOutputError || outputs[0].Error != "bad audio" {
		t.Fatalf("error outputs = %#v", outputs)
	}

	outputs = decoder.Decode([]byte(`{"type":"response.done","response":{"status":"failed","status_details":{"error":{"message":"model failed"}}}}`))
	if len(outputs) != 2 || outputs[0].Type != realtime.ProviderOutputError || outputs[1].Type != realtime.ProviderOutputTurnComplete {
		t.Fatalf("failed response outputs = %#v", outputs)
	}
}

func TestRealtimeURLSetsSelectedModel(t *testing.T) {
	got, err := realtimeURL("wss://api.openai.com/v1/realtime?model=old&trace=1", "gpt-realtime-2.1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "model=gpt-realtime-2.1") || !strings.Contains(got, "trace=1") {
		t.Fatalf("URL = %q", got)
	}
	if _, err := realtimeURL("https://api.openai.com/v1/realtime", defaultModel); err == nil {
		t.Fatal("expected non-WebSocket URL to fail")
	}
}

func requireMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value = %#v, want map[string]any", value)
	}
	return result
}
