package transcript

import (
	"encoding/json"
	"testing"
)

func TestLegacyReasoningKeysAreIgnoredOnDecode(t *testing.T) {
	raw := []byte(`{"kind":"reasoning","reasoning":{"text":"plan","signature":"sig","thought_signature":"old","tool_id":"call-1","responses_data":{"id":"rs_1"}}}`)
	var part MessagePart
	if err := json.Unmarshal(raw, &part); err != nil {
		t.Fatal(err)
	}
	if part.Kind != MessagePartKindReasoning || part.Reasoning == nil || part.Reasoning.Text != "plan" || part.Reasoning.Signature != "sig" {
		t.Fatalf("part=%+v", part)
	}
}
