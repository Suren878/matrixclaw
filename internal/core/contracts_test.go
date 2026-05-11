package core

import (
	"encoding/json"
	"testing"
)

func TestCoreRequestJSONContractsUseStableExternalNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want map[string]any
	}{
		{
			name: "bind client to session",
			in: UseBindingInput{
				Client:      "terminal",
				ExternalKey: "local",
				SessionID:   "session-1",
			},
			want: map[string]any{
				"client":       "terminal",
				"external_key": "local",
				"session_id":   "session-1",
			},
		},
		{
			name: "route user message",
			in: HandleMessageInput{
				Client:           "terminal",
				ExternalKey:      "local",
				SessionID:        "session-1",
				Text:             "hello",
				WorkingDir:       "/tmp/work",
				AllowAutoBindOne: true,
			},
			want: map[string]any{
				"client":              "terminal",
				"external_key":        "local",
				"session_id":          "session-1",
				"text":                "hello",
				"working_dir":         "/tmp/work",
				"allow_auto_bind_one": true,
			},
		},
		{
			name: "select session provider model",
			in: UpdateSessionLLMRequest{
				ProviderID: "openai",
				ModelID:    "gpt-5",
			},
			want: map[string]any{
				"provider_id": "openai",
				"model_id":    "gpt-5",
			},
		},
		{
			name: "execute tool",
			in: ExecuteToolInput{
				SessionID:  "session-1",
				RunID:      "run-1",
				ToolName:   "shell",
				ToolCallID: "call-1",
				WorkingDir: "/tmp/work",
				Approved:   true,
				Args:       json.RawMessage(`{"cmd":"pwd"}`),
			},
			want: map[string]any{
				"session_id":   "session-1",
				"run_id":       "run-1",
				"tool_name":    "shell",
				"tool_call_id": "call-1",
				"working_dir":  "/tmp/work",
				"approved":     true,
				"args":         map[string]any{"cmd": "pwd"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertJSONContains(t, tt.in, tt.want)
		})
	}
}

func TestCoreResponseJSONContractsUseStableEnvelopeNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want map[string]any
	}{
		{
			name: "accepted run",
			in: AcceptRunResult{
				SessionID:   "session-1",
				UserMessage: Message{ID: "message-1"},
				Run:         Run{ID: "run-1"},
			},
			want: map[string]any{
				"session_id": "session-1",
			},
		},
		{
			name: "session models",
			in: SessionModelsResponse{
				ProviderID: "openai",
				ModelID:    "gpt-5",
				Models:     []string{"gpt-5"},
			},
			want: map[string]any{
				"provider_id": "openai",
				"model_id":    "gpt-5",
				"models":      []any{"gpt-5"},
			},
		},
		{
			name: "tool execution",
			in:   ToolExecuteResponse{Result: ExecuteToolResult{ToolCallMessage: Message{ID: "message-1"}}},
			want: map[string]any{
				"result": map[string]any{
					"tool_call_message": map[string]any{"id": "message-1"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertJSONContains(t, tt.in, tt.want)
		})
	}
}

func TestClientDeliveryJSONContractsUseGenericAddress(t *testing.T) {
	t.Parallel()

	address := json.RawMessage(`{"room":"ops","thread":"daemon","message":"restart-99"}`)
	target := ClientDeliveryTarget{
		Client:      "test-client",
		ExternalKey: "ops/restarts",
		SessionID:   "session-1",
		RunID:       "run-1",
		TaskID:      "task-1",
		Summary:     "restart complete",
		Address:     address,
	}
	delivery := ClientDelivery{
		ID:          "delivery-1",
		Type:        ClientDeliveryTypeDaemonRestart,
		Client:      "test-client",
		ExternalKey: "ops/restarts",
		SessionID:   "session-1",
		RunID:       "run-1",
		TaskID:      "task-1",
		Summary:     "restart complete",
		Address:     address,
		Status:      ClientDeliveryStatusPending,
	}

	tests := []struct {
		name string
		in   any
		want map[string]any
	}{
		{
			name: "delivery target",
			in:   target,
			want: map[string]any{
				"client":       "test-client",
				"external_key": "ops/restarts",
				"session_id":   "session-1",
				"run_id":       "run-1",
				"task_id":      "task-1",
				"summary":      "restart complete",
				"address": map[string]any{
					"room":    "ops",
					"thread":  "daemon",
					"message": "restart-99",
				},
			},
		},
		{
			name: "delivery response",
			in: ClientDeliveriesResponse{
				Deliveries: []ClientDelivery{delivery},
			},
			want: map[string]any{
				"deliveries": []any{
					map[string]any{
						"id":           "delivery-1",
						"type":         ClientDeliveryTypeDaemonRestart,
						"client":       "test-client",
						"external_key": "ops/restarts",
						"session_id":   "session-1",
						"run_id":       "run-1",
						"task_id":      "task-1",
						"summary":      "restart complete",
						"address": map[string]any{
							"room":    "ops",
							"thread":  "daemon",
							"message": "restart-99",
						},
						"status": ClientDeliveryStatusPending,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertJSONContains(t, tt.in, tt.want)
			assertJSONOmitsKeys(t, tt.in, "chat_id", "thread_id", "message_id", "payload")
		})
	}
}

func assertJSONContains(t *testing.T, in any, want map[string]any) {
	t.Helper()

	payload, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok {
			t.Fatalf("missing key %q in payload %s", key, payload)
		}
		if !jsonContains(gotValue, wantValue) {
			t.Fatalf("%s = %#v, want to contain %#v; full payload %s", key, gotValue, wantValue, payload)
		}
	}
}

func assertJSONOmitsKeys(t *testing.T, in any, forbidden ...string) {
	t.Helper()

	payload, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	for _, key := range forbidden {
		if jsonHasKey(got, key) {
			t.Fatalf("payload contains forbidden key %q: %s", key, payload)
		}
	}
}

func jsonHasKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, ok := typed[key]; ok {
			return true
		}
		for _, child := range typed {
			if jsonHasKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonHasKey(child, key) {
				return true
			}
		}
	}
	return false
}

func jsonContains(got any, want any) bool {
	switch wantValue := want.(type) {
	case map[string]any:
		gotValue, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for key, childWant := range wantValue {
			childGot, ok := gotValue[key]
			if !ok || !jsonContains(childGot, childWant) {
				return false
			}
		}
		return true
	case []any:
		gotValue, ok := got.([]any)
		if !ok || len(gotValue) < len(wantValue) {
			return false
		}
		for i, childWant := range wantValue {
			if !jsonContains(gotValue[i], childWant) {
				return false
			}
		}
		return true
	default:
		gotPayload, err := json.Marshal(got)
		if err != nil {
			return false
		}
		wantPayload, err := json.Marshal(want)
		if err != nil {
			return false
		}
		return string(gotPayload) == string(wantPayload)
	}
}
