package transcript

import (
	"encoding/json"
	"testing"
)

func TestToolResultPartReadsTheLegacyErrorFlag(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{`{"tool_call_id":"a","name":"bash","content":"x","is_error":true}`, "error"},
		{`{"tool_call_id":"a","name":"bash","content":"x","status":"neutral","is_error":true}`, "neutral"},
		{`{"tool_call_id":"a","name":"bash","content":"x"}`, ""},
	} {
		var part ToolResultPart
		if err := json.Unmarshal([]byte(tc.raw), &part); err != nil {
			t.Fatal(err)
		}
		if part.Status != tc.want || part.ToolCallID != "a" || part.Content != "x" {
			t.Fatalf("%s -> %+v, want status %q", tc.raw, part, tc.want)
		}
	}
	out, err := json.Marshal(ToolResultPart{ToolCallID: "a", Status: "error"})
	if err != nil || string(out) != `{"tool_call_id":"a","name":"","content":"","status":"error"}` {
		t.Fatalf("marshal = %s, %v", out, err)
	}
}
