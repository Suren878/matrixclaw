package chat

import (
	"strings"
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestGenericPendingWebSearchShowsQuery(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
		ID:    "tool_1",
		Name:  "web_search",
		Input: `{"query":"amnezia vpn 2.0 release","limit":5}`,
	}, nil, false)

	rendered := item.RawRender(120)
	for _, want := range []string{"Search Web", "amnezia vpn 2.0 release", "limit=5"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("pending web_search render missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, `{"query"`) {
		t.Fatalf("pending web_search render leaked raw JSON:\n%s", rendered)
	}
}

func TestGenericToolParamsUseHumanPrimaryFields(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   []string
	}{
		{
			name:   "web_fetch",
			params: map[string]any{"url": "https://example.com/page", "task": "extract pricing"},
			want:   []string{"https://example.com/page", "task", "extract pricing"},
		},
		{
			name:   "web_research",
			params: map[string]any{"task": "Find MatrixClaw browser docs", "depth": "quick", "max_sources": float64(3)},
			want:   []string{"Find MatrixClaw browser docs", "depth", "quick", "max_sources", "3"},
		},
		{
			name:   "mcp_browser_navigate",
			params: map[string]any{"url": "https://example.com", "ref": "page"},
			want:   []string{"https://example.com", "ref", "page"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(genericToolParams(tt.name, tt.params), "\n")
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("genericToolParams(%s) = %#v, missing %q", tt.name, got, want)
				}
			}
		})
	}
}
