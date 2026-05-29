package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfacechat "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	surfaceinput "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/input"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRebuildChatFollowsNewOutputWhenFollowing(t *testing.T) {
	m := newRuntimeChatTestModel(t, runtimeChatTestMessages(8)...)
	m.rebuildChat()
	m.chat.ScrollToBottom()
	before := m.chat.View()

	m.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session",
		Messages:  runtimeChatTestMessages(9),
	})
	m.rebuildChat()

	if !m.chat.Follow() {
		t.Fatalf("Follow() = false after rebuild while following, want true")
	}
	if !m.chat.AtBottom() {
		t.Fatalf("AtBottom() = false after rebuild while following, want true")
	}
	if got := m.chat.View(); got == before || !strings.Contains(got, "message 09") {
		t.Fatalf("View() after rebuild =\n%s\nwant newly appended message visible", got)
	}
}

func TestRebuildChatPreservesViewportWhenNotFollowing(t *testing.T) {
	m := newRuntimeChatTestModel(t, runtimeChatTestMessages(9)...)
	m.rebuildChat()
	m.chat.ScrollBy(-6)
	before := m.chat.View()

	m.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session",
		Messages:  runtimeChatTestMessages(10),
	})
	m.rebuildChat()

	if m.chat.Follow() {
		t.Fatalf("Follow() = true after rebuild while above bottom, want false")
	}
	if got := m.chat.View(); got != before {
		t.Fatalf("View() after rebuild =\n%s\nwant\n%s", got, before)
	}
}

func TestSubmitEnablesFollowAndScrollsToBottom(t *testing.T) {
	m := newRuntimeChatTestModel(t, runtimeChatTestMessages(9)...)
	m.rebuildChat()
	m.chat.ScrollBy(-6)
	if m.chat.Follow() {
		t.Fatalf("test setup left follow enabled")
	}

	cmd := m.handleSubmit(surfaceinput.SubmitMsg{Content: "new user message"})

	if cmd == nil {
		t.Fatalf("handleSubmit returned nil cmd, want send command")
	}
	if !m.chat.Follow() {
		t.Fatalf("Follow() = false after submit, want true")
	}
	if !m.chat.AtBottom() {
		t.Fatalf("AtBottom() = false after submit, want true")
	}
}

func TestBuildChatModelMergesRunningAsyncSubagentAfterSpawnResult(t *testing.T) {
	sty := defaultRuntimeChatTestStyles()
	chatModel := buildChatModel(&sty, viewmodel.Snapshot{
		SessionID: "session",
		Messages: []surfacemessage.Message{
			{
				ID:        "assistant",
				Role:      surfacemessage.Assistant,
				SessionID: "session",
				RunID:     "run",
				Parts: []surfacemessage.ContentPart{
					surfacemessage.ToolCall{
						ID:       "spawn-call",
						Name:     "spawn_subagent",
						Input:    `{"name":"Repo scan","goal":"Inspect failing tests"}`,
						Finished: true,
					},
				},
			},
			{
				ID:        "tool",
				Role:      surfacemessage.Tool,
				SessionID: "session",
				RunID:     "run",
				Parts: []surfacemessage.ContentPart{
					surfacemessage.ToolResult{
						ToolCallID: "spawn-call",
						Name:       "spawn_subagent",
						Content:    "Subagent Repo scan started",
						Metadata:   `{"display_name":"Repo scan","status":"running"}`,
						Status:     "neutral",
					},
				},
			},
		},
		Subagents: []core.SubagentTask{{
			ID:               "subagent",
			AgentName:        "Neo",
			DisplayName:      "Repo scan",
			Mode:             core.SubagentTaskModeAsync,
			ParentSessionID:  "session",
			ParentRunID:      "run",
			ParentToolCallID: "spawn-call",
			Runtime:          "matrixclaw",
			Goal:             "Inspect failing tests",
			Status:           core.SubagentTaskStatusRunning,
			CreatedAt:        time.Unix(10, 0),
			UpdatedAt:        time.Unix(11, 0),
		}},
	})
	chatModel.SetSize(100, 20)

	item, ok := chatModel.MessageItem("spawn-call").(surfacechat.ToolMessageItem)
	if !ok {
		t.Fatalf("spawn-call item = %T, want ToolMessageItem", chatModel.MessageItem("spawn-call"))
	}
	if item.Status() != surfacechat.ToolStatusRunning {
		t.Fatalf("spawn-call status = %v, want running", item.Status())
	}
	rendered := chatModel.View()
	for _, want := range []string{"◇ Neo working...", "Repo scan", "Inspect failing tests"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("chat render missing %q:\n%s", want, rendered)
		}
	}
}

func newRuntimeChatTestModel(t *testing.T, messages ...core.Message) *appModel {
	t.Helper()
	m := newApp(context.Background(), nil)
	m.loading = false
	m.session = "session"
	m.width = 80
	m.height = 20
	m.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session",
		Messages:  messages,
	})
	return m
}

func defaultRuntimeChatTestStyles() surfacestyles.Styles {
	return surfacestyles.DefaultStyles()
}

func runtimeChatTestMessages(count int) []core.Message {
	messages := make([]core.Message, 0, count)
	for i := 1; i <= count; i++ {
		role := core.MessageRoleAssistant
		if i%2 == 1 {
			role = core.MessageRoleUser
		}
		messages = append(messages, core.Message{
			ID:        fmt.Sprintf("message-%02d", i),
			SessionID: "session",
			Role:      role,
			Content:   fmt.Sprintf("message %02d\nline two", i),
			CreatedAt: time.Unix(int64(i), 0),
			UpdatedAt: time.Unix(int64(i), 0),
		})
	}
	return messages
}
