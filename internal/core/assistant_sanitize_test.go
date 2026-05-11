package core

import (
	"strings"
	"testing"
)

func TestSanitizeAssistantOutputRemovesReasoningTags(t *testing.T) {
	input := "<thought>\nprivate reasoning\n</thought>\n\nFinal answer."
	got := sanitizeAssistantOutput(input)
	if got != "Final answer." {
		t.Fatalf("sanitizeAssistantOutput() = %q, want final answer only", got)
	}
}

func TestSanitizeAssistantOutputRemovesSeveralReasoningFormats(t *testing.T) {
	input := strings.Join([]string{
		"<think>hidden</think>",
		"<analysis hidden=\"true\">also hidden</analysis>",
		"Thinking...",
		"step one",
		"...done Thinking!",
		"Visible answer.",
	}, "\n")
	got := sanitizeAssistantOutput(input)
	if got != "Visible answer." {
		t.Fatalf("sanitizeAssistantOutput() = %q, want Visible answer.", got)
	}
}

func TestSanitizeAssistantOutputDropsUnclosedReasoningBlock(t *testing.T) {
	input := "Visible before.\n<think>never closed"
	got := sanitizeAssistantOutput(input)
	if got != "Visible before." {
		t.Fatalf("sanitizeAssistantOutput() = %q, want visible prefix", got)
	}
}

func TestAssistantStreamSanitizerDoesNotLeakSplitReasoningTag(t *testing.T) {
	sanitizer := newAssistantStreamSanitizer()
	var out strings.Builder
	for _, delta := range []string{"<tho", "ught>secret", "</thought>\nFi", "nal"} {
		out.WriteString(sanitizer.Push(delta))
	}
	if got := out.String(); got != "Final" {
		t.Fatalf("stream output = %q, want Final", got)
	}
}

func TestAssistantStreamSanitizerHoldsPartialThinkPrefix(t *testing.T) {
	sanitizer := newAssistantStreamSanitizer()
	if got := sanitizer.Push("hello <thi"); got != "hello " {
		t.Fatalf("first stream output = %q, want held partial tag", got)
	}
	if got := sanitizer.Push("nk>secret</think> world"); got != " world" {
		t.Fatalf("second stream output = %q, want visible suffix", got)
	}
}
