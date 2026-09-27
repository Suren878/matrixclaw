package anthropic

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var testTools = []providers.ToolDefinition{
	{Name: "ls", Description: "List a directory", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
	{Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
}

// startServer answers the i-th request with replies[i] and records request bodies.
func startServer(t *testing.T, replies ...string) (*httptest.Server, func(int) []byte) {
	t.Helper()
	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		mu.Lock()
		index := len(bodies)
		bodies = append(bodies, body)
		mu.Unlock()
		if index >= len(replies) {
			http.Error(w, `{"error":{"message":"unexpected request"}}`, http.StatusInternalServerError)
			return
		}
		if strings.HasPrefix(replies[index], "event:") {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = io.WriteString(w, replies[index])
	}))
	t.Cleanup(server.Close)
	return server, func(i int) []byte {
		mu.Lock()
		defer mu.Unlock()
		if i >= len(bodies) {
			t.Fatalf("request %d was not sent (%d sent)", i, len(bodies))
		}
		return bodies[i]
	}
}

func newTestRuntime(t *testing.T, replies ...string) (providers.Runtime, func(int) []byte) {
	t.Helper()
	server, body := startServer(t, replies...)
	runtime, err := New(context.Background(), Config{
		APIKey: "test-key", BaseURL: server.URL, Model: "claude-test", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, body
}

func decodeSent(t *testing.T, body []byte) anthropicRequest {
	t.Helper()
	var request anthropicRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode sent request: %v\n%s", err, body)
	}
	return request
}

func blockTypes(blocks []anthropicBlock) string {
	types := make([]string, 0, len(blocks))
	for _, block := range blocks {
		types = append(types, block.Type)
	}
	return strings.Join(types, ",")
}

func textReply(text string) string {
	return `{"content":[{"type":"text","text":"` + text + `"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
}

func TestToolRoundTripSendsThinkingToolUseAndGroupedResults(t *testing.T) {
	runtime, sent := newTestRuntime(t,
		`{"content":[{"type":"thinking","thinking":"Need both.","signature":"sig-1"},{"type":"redacted_thinking","data":"opaque"},{"type":"text","text":"Checking."},`+
			`{"type":"tool_use","id":"toolu_1","name":"ls","input":{"path":"."}},{"type":"tool_use","id":"toolu_2","name":"read","input":{"path":"go.mod"}}],`+
			`"stop_reason":"tool_use","usage":{"input_tokens":10,"cache_creation_input_tokens":100,"output_tokens":20}}`,
		`{"content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn","usage":{"input_tokens":30,"cache_read_input_tokens":110,"output_tokens":5}}`,
	)
	user := providers.Message{Role: "user", Content: "Inspect the module"}
	first, err := runtime.Generate(context.Background(), providers.Request{SystemPrompt: "You are terse.", Messages: []providers.Message{user}, Tools: testTools})
	if err != nil {
		t.Fatal(err)
	}
	if first.StopReason != providers.StopToolUse || first.Text != "Checking." || len(first.ToolCalls) != 2 {
		t.Fatalf("first response = %#v", first)
	}
	if call := first.ToolCalls[1]; call.ID != "toolu_2" || call.Name != "read" || string(call.Arguments) != `{"path":"go.mod"}` {
		t.Fatalf("second tool call = %#v", call)
	}
	wantReasoning := []providers.ReasoningBlock{{Text: "Need both.", Signature: "sig-1"}, {RedactedData: "opaque"}}
	if len(first.Reasoning) != 2 || first.Reasoning[0] != wantReasoning[0] || first.Reasoning[1] != wantReasoning[1] {
		t.Fatalf("reasoning = %#v", first.Reasoning)
	}
	if first.Usage.PromptTokens != 110 || first.Usage.CacheWriteTokens != 100 || first.Usage.OutputTokens != 20 {
		t.Fatalf("usage = %#v", first.Usage)
	}
	firstSent := decodeSent(t, sent(0))
	if len(firstSent.Tools) != 2 || firstSent.ToolChoice != nil || len(firstSent.System) != 1 || firstSent.System[0].Text != "You are terse." {
		t.Fatalf("first request = %s", sent(0))
	}

	// Stage 1 core replays a tool step as one assistant message followed by its results.
	second, err := runtime.Generate(context.Background(), providers.Request{
		SystemPrompt: "You are terse.",
		Tools:        testTools,
		Messages: []providers.Message{
			user,
			{Role: "assistant", Content: first.Text, Reasoning: first.Reasoning, ToolCalls: first.ToolCalls},
			{Role: "tool", ToolCallID: "toolu_1", Content: "main.go"},
			{Role: "tool", ToolCallID: "toolu_2", Content: "open go.mod: permission denied", IsError: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Text != "Done." || second.StopReason != providers.StopEndTurn || second.Usage.CacheReadTokens != 110 || second.Usage.PromptTokens != 140 {
		t.Fatalf("second response = %#v", second)
	}
	request := decodeSent(t, sent(1))
	if len(request.Messages) != 3 {
		t.Fatalf("messages = %s", sent(1))
	}
	assistant := request.Messages[1]
	if assistant.Role != "assistant" || blockTypes(assistant.Content) != "thinking,redacted_thinking,text,tool_use,tool_use" {
		t.Fatalf("assistant turn = %s", sent(1))
	}
	if thinking := assistant.Content[0]; thinking.Thinking == nil || *thinking.Thinking != "Need both." || thinking.Signature != "sig-1" {
		t.Fatalf("thinking block = %#v", thinking)
	}
	if redacted := assistant.Content[1]; redacted.Data != "opaque" {
		t.Fatalf("redacted block = %#v", redacted)
	}
	if use := assistant.Content[4]; use.ID != "toolu_2" || use.Name != "read" || string(use.Input) != `{"path":"go.mod"}` {
		t.Fatalf("tool_use block = %#v", use)
	}
	results := request.Messages[2]
	if results.Role != "user" || blockTypes(results.Content) != "tool_result,tool_result" {
		t.Fatalf("results turn = %s", sent(1))
	}
	if results.Content[0].ToolUseID != "toolu_1" || results.Content[0].IsError || results.Content[0].Content != "main.go" {
		t.Fatalf("first result = %#v", results.Content[0])
	}
	if results.Content[1].ToolUseID != "toolu_2" || !results.Content[1].IsError || results.Content[1].Content != "open go.mod: permission denied" {
		t.Fatalf("second result = %#v", results.Content[1])
	}
}

func TestEmptyThinkingKeepsItsKeyOnReplay(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("ok"))
	_, err := runtime.Generate(context.Background(), providers.Request{Tools: testTools, Messages: []providers.Message{
		{Role: "user", Content: "List"},
		{Role: "assistant", Reasoning: []providers.ReasoningBlock{{Signature: "sig-omitted"}}, ToolCalls: []providers.ToolCall{{ID: "toolu_1", Name: "ls", Arguments: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolCallID: "toolu_1", Content: "a.go"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sent(0)), `{"type":"thinking","thinking":"","signature":"sig-omitted"}`) {
		t.Fatalf("empty thinking block not replayed verbatim: %s", sent(0))
	}
}

func TestMessagesMergeIntoValidTurns(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("ok"))
	_, err := runtime.Generate(context.Background(), providers.Request{
		Tools: testTools,
		Messages: []providers.Message{
			{Role: "user", Content: "List"},
			{Role: "assistant", Reasoning: []providers.ReasoningBlock{{Text: "unsigned text from another provider"}}, ToolCalls: []providers.ToolCall{
				{ID: "functions.ls:0", Name: "ls", Arguments: json.RawMessage(`not json`)},
			}},
			{Role: "tool", ToolCallID: "functions.ls:0", Content: "a.go"},
			{Role: "user", Content: "Also check README"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := decodeSent(t, sent(0))
	if len(request.Messages) != 3 {
		t.Fatalf("messages = %s", sent(0))
	}
	assistant := request.Messages[1].Content
	if blockTypes(assistant) != "tool_use" || assistant[0].ID != "functions_ls_0" || string(assistant[0].Input) != `{}` {
		t.Fatalf("assistant turn = %s", sent(0))
	}
	user := request.Messages[2].Content
	if blockTypes(user) != "tool_result,text" || user[0].ToolUseID != "functions_ls_0" || user[1].Text != "Also check README" {
		t.Fatalf("user turn = %s", sent(0))
	}
}

func TestToolChoiceNoneKeepsToolsDefined(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("Summary."), textReply("Plain."))
	messages := []providers.Message{{Role: "user", Content: "Wrap up"}}
	if _, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, Tools: testTools, ToolChoice: providers.ToolChoiceNone}); err != nil {
		t.Fatal(err)
	}
	withTools := decodeSent(t, sent(0))
	if withTools.ToolChoice == nil || withTools.ToolChoice.Type != "none" || len(withTools.Tools) != 2 {
		t.Fatalf("request = %s", sent(0))
	}
	if _, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, ToolChoice: providers.ToolChoiceNone}); err != nil {
		t.Fatal(err)
	}
	if withoutTools := decodeSent(t, sent(1)); withoutTools.ToolChoice != nil || len(withoutTools.Tools) != 0 {
		t.Fatalf("request = %s", sent(1))
	}
}

func TestCacheBreakpointsMarkToolsSystemAndTheLatestTurns(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("ok"), textReply("ok"))
	request := providers.Request{
		CacheKey:     "session-1",
		SystemPrompt: "System rules.",
		Tools:        testTools,
		Messages: []providers.Message{
			{Role: "user", Content: "Start"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "toolu_1", Name: "ls", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolCallID: "toolu_1", Content: "a.go"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "toolu_2", Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}}},
			{Role: "tool", ToolCallID: "toolu_2", Content: "package a"},
		},
	}
	if _, err := runtime.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(sent(0)), `"cache_control"`); got != 4 {
		t.Fatalf("cache_control count = %d, want 4: %s", got, sent(0))
	}
	body := decodeSent(t, sent(0))
	marked := func(block anthropicBlock) bool {
		return block.CacheControl != nil && block.CacheControl.Type == "ephemeral"
	}
	if body.Tools[1].CacheControl == nil || body.Tools[0].CacheControl != nil || !marked(body.System[0]) {
		t.Fatalf("tools/system breakpoints wrong: %s", sent(0))
	}
	turns := body.Messages
	if len(turns) != 5 || !marked(turns[4].Content[len(turns[4].Content)-1]) || !marked(turns[2].Content[len(turns[2].Content)-1]) || marked(turns[0].Content[0]) {
		t.Fatalf("message breakpoints wrong: %s", sent(0))
	}
	request.CacheKey = ""
	if _, err := runtime.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sent(1)), "cache_control") {
		t.Fatalf("request without cache key has breakpoints: %s", sent(1))
	}
}

func pngBase64(t *testing.T, width, height int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestUserImagesBecomeBase64Blocks(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("A cat."))
	data := pngBase64(t, 2, 2)
	_, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{
		Role: "user", Content: "What is this?",
		Images: []providers.ImageContent{{MIMEType: "image/png; charset=binary", DataBase64: data}, {MIMEType: "image/jpeg", DataBase64: data}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	content := decodeSent(t, sent(0)).Messages[0].Content
	if blockTypes(content) != "image,image,text" {
		t.Fatalf("content = %s", sent(0))
	}
	for _, block := range content[:2] {
		if source := block.Source; source == nil || source.Type != "base64" || source.MediaType != "image/png" || source.Data != data {
			t.Fatalf("image source = %#v", block.Source)
		}
	}
}

func TestImagesAnthropicWouldRejectAreReplacedByANote(t *testing.T) {
	for _, tc := range []struct {
		name  string
		image providers.ImageContent
		note  string
	}{
		{"over 5 MB", providers.ImageContent{MIMEType: "image/webp", DataBase64: strings.Repeat("A", 5*1024*1024+4)}, "larger than 5 MB"},
		{"unsupported type", providers.ImageContent{MIMEType: "image/bmp", DataBase64: "Qk0="}, "unsupported media type image/bmp"},
		{"missing type", providers.ImageContent{DataBase64: pngBase64(t, 1, 1)}, "missing media type"},
		{"too wide", providers.ImageContent{MIMEType: "image/png", DataBase64: pngBase64(t, 8001, 1)}, "8001x1 px exceeds 8000 px"},
		{"unreadable", providers.ImageContent{MIMEType: "image/png", DataBase64: "iVBORw0KGgo="}, "unreadable image data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, sent := newTestRuntime(t, textReply("ok"))
			_, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "Look", Images: []providers.ImageContent{tc.image}}}})
			if err != nil {
				t.Fatal(err)
			}
			content := decodeSent(t, sent(0)).Messages[0].Content
			if blockTypes(content) != "text,text" || content[0].Text != "[image omitted: "+tc.note+"]" || content[1].Text != "Look" {
				t.Fatalf("content = %#v", content)
			}
		})
	}
}

func TestThinkingIsReplayedOnlyInTheCurrentToolLoop(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("ok"))
	_, err := runtime.Generate(context.Background(), providers.Request{Tools: testTools, Messages: []providers.Message{
		{Role: "user", Content: "First task"},
		{Role: "assistant", Content: "Old step.", Reasoning: []providers.ReasoningBlock{{Text: "old", Signature: "sig-old"}}, ToolCalls: []providers.ToolCall{{ID: "toolu_1", Name: "ls"}}},
		{Role: "tool", ToolCallID: "toolu_1", Content: "a.go"},
		{Role: "assistant", Content: "Old answer.", Reasoning: []providers.ReasoningBlock{{RedactedData: "old-redacted"}}},
		{Role: "user", Content: "Second task"},
		{Role: "assistant", Content: "  Checking.\n", Reasoning: []providers.ReasoningBlock{{Text: "new", Signature: "sig-new"}}, ToolCalls: []providers.ToolCall{{ID: "toolu_2", Name: "ls"}}},
		{Role: "tool", ToolCallID: "toolu_2", Content: "b.go"},
		{Role: "user", Content: "Also look at c.go"},
		{Role: "assistant", Reasoning: []providers.ReasoningBlock{{RedactedData: "new-redacted"}}, ToolCalls: []providers.ToolCall{{ID: "toolu_3", Name: "read"}}},
		{Role: "tool", ToolCallID: "toolu_3", Content: "package c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body := string(sent(0))
	if strings.Contains(body, "sig-old") || strings.Contains(body, "old-redacted") {
		t.Fatalf("thinking before the last user message was replayed: %s", body)
	}
	turns := decodeSent(t, sent(0)).Messages
	if len(turns) != 9 {
		t.Fatalf("turns = %s", body)
	}
	if got := blockTypes(turns[1].Content) + "|" + blockTypes(turns[3].Content); got != "text,tool_use|text" {
		t.Fatalf("old assistant turns = %s", got)
	}
	current := turns[5].Content
	if blockTypes(current) != "thinking,text,tool_use" || current[0].Signature != "sig-new" || current[1].Text != "  Checking.\n" {
		t.Fatalf("current loop turn = %#v", current)
	}
	if blockTypes(turns[6].Content) != "tool_result,text" || blockTypes(turns[7].Content) != "redacted_thinking,tool_use" {
		t.Fatalf("steered loop turns = %s", body)
	}
}

func TestSanitizedToolIDsStayDistinctAndPaired(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("ok"))
	_, err := runtime.Generate(context.Background(), providers.Request{Tools: testTools, Messages: []providers.Message{
		{Role: "user", Content: "List"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "functions.ls:0", Name: "ls"}, {ID: "functions_ls_0", Name: "ls"}, {ID: "", Name: "read"}}},
		{Role: "tool", ToolCallID: "functions_ls_0", Content: "b"},
		{Role: "tool", ToolCallID: "functions.ls:0", Content: "a"},
		{Role: "tool", ToolCallID: "", Content: "c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	turns := decodeSent(t, sent(0)).Messages
	uses, results := turns[1].Content, turns[2].Content
	if blockTypes(results) != "tool_result,tool_result,tool_result" {
		t.Fatalf("results = %s", sent(0))
	}
	got := []string{uses[0].ID, uses[1].ID, uses[2].ID, results[0].ToolUseID, results[1].ToolUseID, results[2].ToolUseID}
	want := []string{"functions_ls_0", "functions_ls_0-2", "toolu_missing", "functions_ls_0-2", "functions_ls_0", "toolu_missing"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

func TestStopReasonsWithThinkingAndTools(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		want        providers.StopReason
		calls       int
	}{
		{"output limit spent on thinking", `{"content":[{"type":"thinking","thinking":"long","signature":"s"}],"stop_reason":"max_tokens"}`, providers.StopMaxTokens, 0},
		{"context window exceeded", `{"content":[{"type":"text","text":"Cut"}],"stop_reason":"model_context_window_exceeded"}`, providers.StopMaxTokens, 0},
		{"end_turn with tool calls", `{"content":[{"type":"tool_use","id":"toolu_1","name":"ls","input":{}}],"stop_reason":"end_turn"}`, providers.StopToolUse, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, _ := newTestRuntime(t, tc.reply)
			response, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "Go"}}, Tools: testTools})
			if err != nil {
				t.Fatal(err)
			}
			if response.StopReason != tc.want || len(response.ToolCalls) != tc.calls {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestToolUseWithoutNameIsAMalformedCall(t *testing.T) {
	runtime, _ := newTestRuntime(t, `{"content":[{"type":"tool_use","id":"toolu_1","input":{}}],"stop_reason":"tool_use"}`)
	_, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "Go"}}, Tools: testTools})
	if !errors.Is(err, providers.ErrMalformedToolCall) {
		t.Fatalf("error = %v, want ErrMalformedToolCall", err)
	}
}

func TestDisabledToolUseModeStripsToolsAndToolTurns(t *testing.T) {
	server, sent := startServer(t, textReply("Plain."))
	runtime, err := New(context.Background(), Config{
		APIKey: "test-key", BaseURL: server.URL, Model: "claude-test", HTTPClient: server.Client(),
		ToolUseMode: providers.ToolUseDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Generate(context.Background(), providers.Request{
		Tools: testTools,
		Messages: []providers.Message{
			{Role: "user", Content: "Inspect"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "toolu_1", Name: "ls", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolCallID: "toolu_1", Content: "a.go"},
			{Role: "user", Content: "continue"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(sent(0))
	for _, forbidden := range []string{`"tools"`, "tool_use", "tool_result"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("disabled tool use still sends %s: %s", forbidden, body)
		}
	}
}

// rejectingServer answers 400 with message while the request matches reject,
// and textReply otherwise; it returns the recorded request bodies.
func rejectingServer(t *testing.T, reject func(body string) bool, message string) (providers.Runtime, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		if reject(string(body)) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"`+message+`"}}`)
			return
		}
		_, _ = io.WriteString(w, textReply("ok"))
	}))
	t.Cleanup(server.Close)
	runtime, err := New(context.Background(), Config{APIKey: "k", BaseURL: server.URL, Model: "claude-test", HTTPClient: server.Client(), ToolUseMode: providers.ToolUseNative})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, &bodies
}

var loopWithThinking = providers.Request{Tools: testTools, Messages: []providers.Message{
	{Role: "user", Content: "List"},
	{Role: "assistant", Reasoning: []providers.ReasoningBlock{{Text: "t", Signature: "sig"}, {RedactedData: "r"}}, ToolCalls: []providers.ToolCall{{ID: "toolu_1", Name: "ls"}}},
	{Role: "tool", ToolCallID: "toolu_1", Content: "a.go"},
}}

func TestRejectedThinkingIsRetriedOnceWithoutIt(t *testing.T) {
	hasThinking := func(body string) bool { return strings.Contains(body, "thinking") }
	runtime, bodies := rejectingServer(t, hasThinking, "messages.1.content.0: Invalid `signature` in `thinking` block")
	response, err := runtime.Generate(context.Background(), loopWithThinking)
	if err != nil || response.Text != "ok" {
		t.Fatalf("response = %#v err = %v", response, err)
	}
	if len(*bodies) != 2 || hasThinking((*bodies)[1]) || !strings.Contains((*bodies)[1], `"tool_use"`) {
		t.Fatalf("requests = %q", *bodies)
	}
}

func TestOtherBadRequestsAreNotRetried(t *testing.T) {
	always := func(string) bool { return true }
	runtime, bodies := rejectingServer(t, always, "max_tokens: 999999 > 64000")
	if _, err := runtime.Generate(context.Background(), loopWithThinking); err == nil || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("error = %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(*bodies))
	}
}
