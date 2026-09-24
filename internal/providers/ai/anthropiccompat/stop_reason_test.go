package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestTextReplyCarriesStopReasonAndOutputLimit(t *testing.T) {
	var limits []float64
	replies := []string{
		`{"content":[{"type":"text","text":"Partial"}],"stop_reason":"max_tokens"}`,
		`{"content":[{"type":"text","text":"Done"}],"stop_reason":"end_turn"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		limits = append(limits, body["max_tokens"].(float64))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(replies[len(limits)-1]))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	messages := []providers.Message{{Role: "user", Content: "hello"}}
	cut, err := runtime.Generate(context.Background(), providers.Request{Messages: messages})
	if err != nil || cut.StopReason != providers.StopMaxTokens || cut.Text != "Partial" {
		t.Fatalf("response=%+v err=%v", cut, err)
	}
	done, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, MaxOutputTokens: 2000})
	if err != nil || done.StopReason != providers.StopEndTurn {
		t.Fatalf("response=%+v err=%v", done, err)
	}
	if limits[0] != float64(providers.DefaultMaxOutputTokens) || limits[1] != 2000 {
		t.Fatalf("max_tokens sent=%v", limits)
	}
}

func TestStreamedRefusalIsAValidEmptyReply(t *testing.T) {
	stream := "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil || response.StopReason != providers.StopRefusal {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestListModelsRegistersOutputLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-maxout-test","max_input_tokens":200000,"max_tokens":8192}]}`))
	}))
	defer server.Close()
	if _, err := ListModels(context.Background(), Config{ProviderID: "anthropic-maxout", APIKey: "test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if got := providers.ResolveModelMetadata("anthropic-maxout", providers.TypeAnthropic, "claude-maxout-test").MaxOutputTokens; got != 8192 {
		t.Fatalf("catalog max output=%d, want 8192", got)
	}
}
