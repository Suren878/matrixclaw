package gemini

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestGenerateStreamsDeltasUntilFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("request went to %s, want the SSE streaming endpoint", r.URL)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"lo"}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":2}}`,
		} {
			_, _ = w.Write([]byte("data: " + chunk + "\r\n\r\n"))
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	var preview strings.Builder
	ctx := providers.WithTextStream(context.Background(), func(delta string) error {
		preview.WriteString(delta)
		return nil
	})
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if preview.String() != "Hello" || response.Text != "Hello" || response.StopReason != providers.StopEndTurn || response.Usage.PromptTokens != 12 {
		t.Fatalf("preview=%q response=%+v", preview.String(), response)
	}
}

func TestStreamIsIncompleteWithoutFinishReasonAndFailsOnErrorChunk(t *testing.T) {
	cut := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Partial"}]}}]}` + "\n\n"
	if _, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(cut)); !errors.Is(err, providers.ErrIncompleteResponse) {
		t.Fatalf("cut stream error=%v, want ErrIncompleteResponse", err)
	}
	failed := `data: {"error":{"code":429,"message":"quota exceeded"}}` + "\n\n"
	if _, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(failed)); err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("error chunk=%v", err)
	}
}

func TestBlockedPromptStopsAsContentFilter(t *testing.T) {
	stream := `data: {"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"},"usageMetadata":{"promptTokenCount":7}}` + "\n\n"
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != providers.StopContentFilter || response.Text != "" || response.Usage.PromptTokens != 7 {
		t.Fatalf("response=%+v", response)
	}
}

func TestStreamErrorChunkNamesItsStatusAndRetriesServerAndRateLimits(t *testing.T) {
	for _, tc := range []struct {
		chunk     string
		want      []string
		retryable bool
	}{
		{`{"error":{"code":503,"status":"UNAVAILABLE","message":"The model is overloaded."}}`, []string{"503", "UNAVAILABLE", "overloaded"}, true},
		{`{"error":{"code":500,"status":"INTERNAL"}}`, []string{"500", "INTERNAL"}, true},
		{`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota exceeded"}}`, []string{"429", "RESOURCE_EXHAUSTED", "quota exceeded"}, true},
		{`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad schema"}}`, []string{"400", "bad schema"}, false},
	} {
		_, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader("data: "+tc.chunk+"\n\n"))
		if err == nil || providers.IsRetryableGenerationError(err) != tc.retryable {
			t.Fatalf("%s: error=%v, want retryable=%v", tc.chunk, err, tc.retryable)
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: error %q lacks %q", tc.chunk, err, want)
			}
		}
	}
}

func TestStreamAssemblesTextAndSignedCallFromSeparateChunks(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Reading a."}]}}]}`,
		`data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{"path":"a"}},"thoughtSignature":"sig-a"}]}}]}`,
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":"STOP"}]}`,
	}, "\n\n") + "\n\n"
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "Reading a." || response.StopReason != providers.StopToolUse {
		t.Fatalf("response=%+v", response)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "read" || string(response.ToolCalls[0].Arguments) != `{"path":"a"}` {
		t.Fatalf("tool calls=%+v", response.ToolCalls)
	}
	if len(response.Reasoning) != 1 || response.Reasoning[0].Signature != "sig-a" {
		t.Fatalf("reasoning=%+v", response.Reasoning)
	}
}
