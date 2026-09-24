package agent

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestToolCallIdentityPreservesLargeIntegers(t *testing.T) {
	for _, tc := range []struct {
		name, left, right string
		equal             bool
	}{
		{"large integer differs", `{"id":9007199254740992}`, `{"id":9007199254740993}`, false},
		{"nested integer differs", `{"items":[{"id":9007199254740992}]}`, `{"items":[{"id":9007199254740993}]}`, false},
		{"key order and whitespace", `{"id":9007199254740993,"ok":true}`, ` {"ok": true, "id":9007199254740993} `, true},
		{"missing arguments", "", `{}`, true},
		{"invalid trailing data", `{} garbage`, `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameRequestedTool("write", []byte(tc.left), providers.ToolCall{Name: "write", Arguments: []byte(tc.right)}); got != tc.equal {
				t.Fatalf("sameRequestedTool=%v, want %v", got, tc.equal)
			}
		})
	}
}
