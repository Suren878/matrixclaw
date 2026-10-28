package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

type fakeStore struct{ saved map[string][]byte }

func (s *fakeStore) ReadTemporaryBytes(path string) (localstorage.TempEntry, []byte, error) {
	data, ok := s.saved[path]
	if !ok {
		return localstorage.TempEntry{}, nil, localstorage.ErrNotFound
	}
	return localstorage.TempEntry{Path: path, MIMEType: "image/png"}, data, nil
}

func (s *fakeStore) ReadBytes(path string, _ int64) (localstorage.Entry, []byte, error) {
	return localstorage.Entry{}, nil, localstorage.ErrNotFound
}

func (s *fakeStore) SaveTemporary(path string, content []byte, title string, _ []string, mimeType string) (localstorage.TempEntry, error) {
	s.saved[path] = content
	return localstorage.TempEntry{Path: path, Title: title, MIMEType: mimeType, Size: int64(len(content))}, nil
}

type fakeDeliveries struct{ created []core.ClientDelivery }

func (d *fakeDeliveries) CreateClientDelivery(_ context.Context, delivery core.ClientDelivery) (core.ClientDelivery, error) {
	delivery.ID = "delivery-1"
	d.created = append(d.created, delivery)
	return delivery, nil
}

func newTestModule(t *testing.T, baseURL string) (*Module, *fakeStore, *fakeDeliveries) {
	t.Helper()
	store, deliveries := &fakeStore{saved: map[string][]byte{}}, &fakeDeliveries{}
	m := New(store, deliveries)
	cfg := setup.Config{
		Providers: []setup.ProviderConfig{
			{ID: "local", Name: "Local", Type: "openai-compatible", BaseURL: baseURL, APIKey: "sk-test"},
			{ID: "anthropic", APIKey: "sk-ant"},
		},
		Modules: setup.ModulesConfig{Image: setup.ImageConfig{Enabled: true, Provider: "local", Model: "qwen-image", Size: "1024x1024"}},
	}
	if err := m.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	return m, store, deliveries
}

func TestGenerateSavesImageAndSendsItToTelegram(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, "wrong request", http.StatusBadRequest)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(pngBytes)}}})
	}))
	defer server.Close()
	m, store, deliveries := newTestModule(t, server.URL+"/v1")

	tool := m.Tools()[0]
	result, err := tool.Execute(context.Background(), tools.Call{Client: "telegram", ExternalKey: "chat:1", Args: json.RawMessage(`{"prompt":"a fox"}`)})
	if err != nil || result.IsError() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got["model"] != "qwen-image" || got["prompt"] != "a fox" || got["size"] != "1024x1024" {
		t.Fatalf("request=%v", got)
	}
	if len(store.saved) != 1 || len(deliveries.created) != 1 {
		t.Fatalf("saved=%d deliveries=%d", len(store.saved), len(deliveries.created))
	}
	var payload core.DocumentDeliveryPayload
	_ = json.Unmarshal(deliveries.created[0].Payload, &payload)
	if !payload.Photo || !payload.Temporary || payload.MIMEType != "image/png" || store.saved[payload.StoragePath] == nil {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestGenerateDownloadsURLAndReportsProviderErrors(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file.png":
			_, _ = w.Write(pngBytes)
		default:
			var request map[string]any
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request["prompt"] == "forbidden" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"message":"The model 'qwen-image' is not enabled for this key.","code":"model_not_allowed"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"url": server.URL + "/file.png"}}})
		}
	}))
	defer server.Close()
	m, store, deliveries := newTestModule(t, server.URL+"/v1")
	tool := m.Tools()[0]

	result, err := tool.Execute(context.Background(), tools.Call{Client: "terminal", Args: json.RawMessage(`{"prompt":"a fox"}`)})
	if err != nil || result.IsError() || len(store.saved) != 1 || len(deliveries.created) != 0 {
		t.Fatalf("result=%+v err=%v saved=%d deliveries=%d", result, err, len(store.saved), len(deliveries.created))
	}
	result, _ = tool.Execute(context.Background(), tools.Call{Args: json.RawMessage(`{"prompt":"forbidden"}`)})
	if !result.IsError() || !strings.Contains(result.Content, "not enabled for this key") {
		t.Fatalf("result=%+v", result)
	}
}

func TestToolsAndProvidersFollowSettings(t *testing.T) {
	m, _, _ := newTestModule(t, "http://127.0.0.1:1/v1")
	provider, ok := modules.Find(m.Settings(context.Background()), []string{"provider"})
	if !ok || len(provider.Options) != 1 || provider.Options[0].Value != "local" {
		t.Fatalf("provider options=%+v", provider.Options)
	}
	change, err := m.Change(context.Background(), []string{"provider"}, "local")
	if err != nil || change.Config == nil {
		t.Fatalf("change=%+v err=%v", change, err)
	}
	if _, err := m.Change(context.Background(), []string{"provider"}, "anthropic"); err == nil {
		t.Fatal("anthropic accepted as an image provider")
	}
	if err := m.Apply(context.Background(), setup.Config{Modules: setup.ModulesConfig{Image: setup.ImageConfig{Provider: "local", Model: "qwen-image"}}}); err != nil {
		t.Fatal(err)
	}
	if len(m.Tools()) != 0 {
		t.Fatal("tool offered while the module is off")
	}
}

func TestEditSendsImagesAndReportsMissingEndpoint(t *testing.T) {
	var gotPrompt, gotModel string
	var gotImage []byte
	edits := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" || !edits {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"Unknown endpoint."}}`))
			return
		}
		file, _, err := r.FormFile("image")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotImage, _ = io.ReadAll(file)
		gotPrompt, gotModel = r.FormValue("prompt"), r.FormValue("model")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(pngBytes)}}})
	}))
	defer server.Close()
	m, store, _ := newTestModule(t, server.URL+"/v1")
	store.saved["telegram/images/photo.png"] = append([]byte(nil), pngBytes...)
	tool := m.Tools()[0]

	result, err := tool.Execute(context.Background(), tools.Call{Args: json.RawMessage(`{"prompt":"dress him as a king","images":["telegram/images/photo.png"]}`)})
	if err != nil || result.IsError() || gotPrompt != "dress him as a king" || gotModel != "qwen-image" || string(gotImage) != string(pngBytes) {
		t.Fatalf("result=%+v err=%v prompt=%q model=%q image=%d bytes", result, err, gotPrompt, gotModel, len(gotImage))
	}
	result, _ = tool.Execute(context.Background(), tools.Call{Args: json.RawMessage(`{"prompt":"x","images":["missing.png"]}`)})
	if !result.IsError() || !strings.Contains(result.Content, "not in storage") {
		t.Fatalf("missing image result=%+v", result)
	}
	edits = false
	result, _ = tool.Execute(context.Background(), tools.Call{Args: json.RawMessage(`{"prompt":"x","images":["telegram/images/photo.png"]}`)})
	if !result.IsError() || !strings.Contains(result.Content, "does not edit images") {
		t.Fatalf("no edits endpoint result=%+v", result)
	}
}
