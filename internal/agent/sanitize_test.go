package agent

import (
	"strings"
	"testing"
	"unicode"
)

func TestSanitizerStripsOnlyReasoningBlocks(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"leading closed block", "<think>plan it</think>\n\nThe answer.", "The answer."},
		{"leading block with attributes", "  <Thinking mode=\"deep\">\nplan\n</THINKING>\nThe answer.", "The answer."},
		{"leading unclosed block", "<thinking>\nstill planning", ""},
		{"ollama block", "Thinking...\nhmm\n...done thinking!\n\nThe answer.", "The answer."},
		{"prose opening like an ollama block", "Thinking... about it, use a map.", "Thinking... about it, use a map."},
		{"closed block on a later line", "Intro.\n<thought>aside</thought>\nRest.", "Intro.\n\nRest."},
		{"unclosed tag on a later line", "Wrap it in a\n<thought> tag and add more text.", "Wrap it in a\n<thought> tag and add more text."},
		{"tag in prose", "Wrap it in a `<thought>` tag and add more text.", "Wrap it in a `<thought>` tag and add more text."},
		{"self-closing JSX", "Render it:\n<Thinking/>\n<Thinking label=\"x\" />\ndone.", "Render it:\n<Thinking/>\n<Thinking label=\"x\" />\ndone."},
		{"JSX in a fence", "```jsx\n<Thinking>\n  {children}\n</Thinking>\n```\nAfter.", "```jsx\n<Thinking>\n  {children}\n</Thinking>\n```\nAfter."},
		{"block after a fence closes", "```\ncode\n```\n<think>x</think>\nAfter.", "```\ncode\n```\n\nAfter."},
		{"longer fence holds a shorter one", "````md\n```\n<think>kept</think>\n```\n````", "````md\n```\n<think>kept</think>\n```\n````"},
		{"other tags", "<thinker>x</thinker> stays", "<thinker>x</thinker> stays"},
		{"line ends and blank lines", "a\r\n\r\n\r\n\r\nb\rc", "a\n\nb\nc"},
		{"blank lines in a fence", "```\na\n\n\n\nb\n```", "```\na\n\n\n\nb\n```"},
		{"non-ASCII before a block", "Привет İ.\n<think>x</think>\nok", "Привет İ.\n\nok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeAssistantOutput(tc.in); got != tc.want {
				t.Fatalf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for _, size := range []int{1, 2, 3, 7} {
				if got := streamInChunks(tc.in, size); got != tc.want {
					t.Fatalf("streamed in %d-byte chunks = %q, want %q", size, got, tc.want)
				}
			}
		})
	}
}

func TestCleanOutputKeepsTheWhitespaceAtBothEnds(t *testing.T) {
	if got := cleanAssistantOutput(" files and\n"); got != " files and\n" {
		t.Fatalf("clean = %q", got)
	}
}

func TestStreamSanitizerHoldsOnlyWhatMayOpenABlock(t *testing.T) {
	var s replySanitizer
	if got := s.Push("Intro.\n<thi"); got != "Intro.\n" {
		t.Fatalf("first push = %q, want the possible tag held", got)
	}
	if got := s.Push("s is a list"); got != "<this is a list" {
		t.Fatalf("second push = %q, want the held text once it cannot be a tag", got)
	}
}

// streamInChunks feeds text to a stream sanitizer size bytes at a time, as a
// provider's deltas would, and trims what a reply's end trims.
func streamInChunks(text string, size int) string {
	var s replySanitizer
	var out strings.Builder
	for len(text) > 0 {
		n := min(size, len(text))
		out.WriteString(s.Push(text[:n]))
		text = text[n:]
	}
	out.WriteString(s.Flush())
	return strings.TrimRightFunc(out.String(), unicode.IsSpace)
}
