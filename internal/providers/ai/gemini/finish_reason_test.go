package gemini

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func generateAgainst(t *testing.T, reply string) (providers.Response, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + reply + "\n\n"))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	return runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
}

func TestFinishReasonBecomesStopReason(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
		want  providers.StopReason
		text  string
	}{
		{"stop", `{"candidates":[{"content":{"role":"model","parts":[{"text":"Done"}]},"finishReason":"STOP"}]}`, providers.StopEndTurn, "Done"},
		{"stop with call", `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{}}}]},"finishReason":"STOP"}]}`, providers.StopToolUse, ""},
		{"max tokens keeps text", `{"candidates":[{"content":{"role":"model","parts":[{"text":" Partial "}]},"finishReason":"MAX_TOKENS"}]}`, providers.StopMaxTokens, " Partial "},
		{"max tokens spent on thinking", `{"candidates":[{"content":{"role":"model"},"finishReason":"MAX_TOKENS"}]}`, providers.StopMaxTokens, ""},
		{"safety", `{"candidates":[{"content":{"role":"model"},"finishReason":"SAFETY"}]}`, providers.StopContentFilter, ""},
		{"prohibited", `{"candidates":[{"content":{"role":"model"},"finishReason":"PROHIBITED_CONTENT"}]}`, providers.StopContentFilter, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := generateAgainst(t, tc.reply)
			if err != nil {
				t.Fatal(err)
			}
			if response.StopReason != tc.want || response.Text != tc.text {
				t.Fatalf("response=%+v, want stop=%q text=%q", response, tc.want, tc.text)
			}
		})
	}
}

func TestMalformedFunctionCallIsRetryable(t *testing.T) {
	_, err := generateAgainst(t, `{"candidates":[{"content":{"role":"model"},"finishReason":"MALFORMED_FUNCTION_CALL"}]}`)
	if !errors.Is(err, providers.ErrMalformedToolCall) || !providers.IsRetryableGenerationError(err) {
		t.Fatalf("error=%v, want retryable ErrMalformedToolCall", err)
	}
}
