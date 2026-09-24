package transcript_test

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func reply(runID, content, finish string) transcript.Message {
	message := transcript.Message{RunID: runID, Role: transcript.MessageRoleAssistant, Content: content}
	if finish != "" {
		message.Parts = []transcript.MessagePart{{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: finish}}}
	}
	return message
}

func engineNote(runID string) transcript.Message {
	return transcript.Message{RunID: runID, Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngineModel, Content: "Continue exactly where you stopped."}
}

func TestRunReplyJoinsRepliesCutByTheOutputLimit(t *testing.T) {
	messages := []transcript.Message{
		{RunID: "run-1", Role: transcript.MessageRoleUser, Content: "Write the report."},
		reply("run-1", "Checked the logs.", "tool_calls"),
		reply("run-1", "The report: part o", "max_tokens"),
		engineNote("run-1"),
		reply("run-1", "ne, part two ", "max_tokens"),
		engineNote("run-1"),
		reply("run-1", "and part three.\n", ""),
		{RunID: "run-2", Role: transcript.MessageRoleUser, Content: "Thanks."},
	}

	if got := transcript.RunReply(messages, "run-1"); got != "The report: part one, part two and part three." {
		t.Fatalf("RunReply = %q", got)
	}
}

func TestRunReplyIsTheLatestAssistantText(t *testing.T) {
	messages := []transcript.Message{
		reply("run-1", "Earlier cut reply", "max_tokens"),
		reply("run-1", "", "tool_calls"),
		reply("run-1", "Final answer.", ""),
		reply("run-1", "", ""),
		reply("run-2", "Another run.", ""),
	}

	if got := transcript.RunReply(messages, "run-1"); got != "Final answer." {
		t.Fatalf("RunReply = %q", got)
	}
	if got := transcript.RunReply(messages, "run-3"); got != "" {
		t.Fatalf("RunReply of a run without replies = %q", got)
	}
}
