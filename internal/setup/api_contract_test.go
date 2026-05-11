package setup

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestProviderSetupResponseJSONKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want []string
	}{
		{
			name: "list",
			in:   ProviderSetupListResponse{},
			want: []string{"providers"},
		},
		{
			name: "item",
			in:   ProviderSetupResponse{},
			want: []string{"provider"},
		},
		{
			name: "ok",
			in:   ProviderSetupOKResponse{},
			want: []string{"ok"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(test.in)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			got := make([]string, 0, len(payload))
			for key := range payload {
				got = append(got, key)
			}
			sort.Strings(got)
			sort.Strings(test.want)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("JSON keys = %v, want %v; payload=%s", got, test.want, data)
			}
		})
	}
}

func TestProviderSetupUpdateJSONKeys(t *testing.T) {
	t.Parallel()

	update := ProviderSetupUpdate{
		Name:        "Local AI",
		Type:        "openai-compatible",
		APIKey:      "secret",
		BaseURL:     "http://127.0.0.1:11434/v1",
		Model:       "llama3",
		ToolUseMode: providers.ToolUseDisabled,
		Active:      true,
	}
	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	got := make([]string, 0, len(payload))
	for key := range payload {
		got = append(got, key)
	}
	want := []string{
		"active",
		"api_key",
		"base_url",
		"model",
		"name",
		"tool_use_mode",
		"type",
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON keys = %v, want %v; payload=%s", got, want, data)
	}
}

func TestProviderSetupUpdateIgnoresLegacyToolSchemaDialect(t *testing.T) {
	t.Parallel()

	var update ProviderSetupUpdate
	if err := json.Unmarshal([]byte(`{
		"name":"Local AI",
		"type":"openai-compatible",
		"tool_use_mode":"disabled",
		"tool_schema_dialect":"gemini"
	}`), &update); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if update.Name != "Local AI" || update.ToolUseMode != providers.ToolUseDisabled {
		t.Fatalf("update = %#v, want known fields decoded", update)
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if jsonContainsKey(t, data, "tool_schema_dialect") {
		t.Fatalf("payload contains legacy tool_schema_dialect: %s", data)
	}
}

func TestProviderSetupItemOmitsProviderDialect(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(ProviderSetupItem{
		ID:          "local-ai",
		Name:        "Local AI",
		Type:        "openai-compatible",
		Status:      "Configured",
		Configured:  true,
		Implemented: true,
		ToolUseMode: providers.ToolUseDisabled,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if jsonContainsKey(t, data, "tool_schema_dialect") {
		t.Fatalf("payload contains provider dialect: %s", data)
	}
}

func jsonContainsKey(t *testing.T, data []byte, key string) bool {
	t.Helper()

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	_, ok := payload[key]
	return ok
}
