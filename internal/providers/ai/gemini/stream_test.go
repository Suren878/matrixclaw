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
