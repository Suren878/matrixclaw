package controlplane

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestProviderPickerItemsSelectsConfiguredProviderWithoutEditingSetup(t *testing.T) {
	items := providerPickerItems([]setup.ProviderSetupItem{
		{ID: "current", Name: "Current", Configured: true},
		{ID: "custom-cloud", Name: "Custom Cloud", Configured: true},
		{ID: "new", Name: "New", Configured: false},
	}, &core.Session{ProviderID: "current", ModelID: "current-model"}, true)

	commands := map[string]string{}
	for _, item := range items {
		commands[item.ID] = item.Command
	}
	if got := commands["current"]; got != "/provider current" {
		t.Fatalf("current provider command = %q, want edit command", got)
	}
	if got := commands["custom-cloud"]; got != "/provider use custom-cloud" {
		t.Fatalf("configured provider command = %q, want session selection command", got)
	}
	if got := commands["new"]; got != "/provider new" {
		t.Fatalf("unconfigured provider command = %q, want setup command", got)
	}
}

func TestSavedProviderFormSelectsTheProviderInTheSession(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.configured = setup.ProviderSetupItem{ID: "custom-cloud", Name: "Custom Cloud", Configured: true}
	runtime.providers = []setup.ProviderSetupItem{
		{ID: "old", Name: "Old", Configured: true},
		{ID: "custom-cloud", Name: "Custom Cloud", Type: "openai-compatible", BaseURL: "http://x", Model: "m", APIKeyPreview: "****1234", Configured: true},
	}
	runtime.updated = core.Session{ID: "session-1", ProviderID: "custom-cloud", ModelID: "custom-model"}
	dispatcher := runtime.dispatcher(core.RoleOwner)
	formID := formIDOf(t, openProviderForm(providerForm{Provider: runtime.providers[1]}))

	result, err := dispatcher.handleProviderForm(context.Background(), &core.Session{ID: "session-1", ProviderID: "old"}, formID+" save")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("saving returned no provider picker")
	}
	for _, item := range result.Picker.Items {
		if item.ID == "custom-cloud" {
			if !item.Selected || item.Info != "custom-model" {
				t.Fatalf("saved provider item = %+v", item)
			}
			return
		}
	}
	t.Fatal("saved provider is missing from picker")
}

func TestSavedProviderFormReportsSessionSelectionFailure(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.configured = setup.ProviderSetupItem{ID: "custom-cloud", Configured: true}
	runtime.updateErr = errors.New("write failed")
	dispatcher := runtime.dispatcher(core.RoleOwner)
	formID := formIDOf(t, openProviderForm(providerForm{Provider: setup.ProviderSetupItem{ID: "custom-cloud", Type: "openai-compatible", Name: "Custom Cloud", BaseURL: "http://x", Model: "m", APIKeyPreview: "****1234", Configured: true}}))

	if _, err := dispatcher.handleProviderForm(context.Background(), &core.Session{ID: "session-1"}, formID+" save"); err == nil {
		t.Fatal("session selection failure was ignored")
	}
}

func TestCustomProviderFormNeverPutsTheKeyInCommands(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.configured = setup.ProviderSetupItem{ID: "local-ai", Configured: true}
	dispatcher := runtime.dispatcher(core.RoleOwner)
	ctx := context.Background()
	result := openCustomProviderForm("openai")
	formID := formIDOf(t, result)
	results := []Result{result}
	for _, step := range []string{"field key", "set key sk-secret-1234", "set name Local AI", "set base http://127.0.0.1:8000/v1", "set model qwen", "field key", "field model", ""} {
		result, err := dispatcher.handleProviderForm(ctx, nil, formID+" "+step)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		results = append(results, result)
	}
	for _, result := range results {
		for _, text := range resultStrings(result) {
			if strings.Contains(text, "sk-secret") {
				t.Fatalf("form output carries the key: %q", text)
			}
		}
	}

	if _, err := dispatcher.handleProviderForm(ctx, nil, formID+" save"); err != nil {
		t.Fatal(err)
	}
	update := runtime.lastUpdate
	if runtime.lastID != "local-ai" || deref(update.APIKey) != "sk-secret-1234" || deref(update.Name) != "Local AI" || deref(update.Model) != "qwen" || deref(update.Type) != "openai-compatible" || !update.Active {
		t.Fatalf("saved %q %+v", runtime.lastID, update)
	}
	expired, _ := dispatcher.handleProviderForm(ctx, nil, formID)
	if !strings.Contains(expired.Text, "expired") {
		t.Fatalf("form still open after save: %+v", expired)
	}
}

func TestProviderFormStoreExpiresAndStaysBounded(t *testing.T) {
	now := time.Unix(0, 0)
	store := &formStore{forms: map[string]storedProviderForm{}, ttl: time.Minute, limit: 2, now: func() time.Time { return now }}
	first := store.open(providerForm{})
	now = now.Add(time.Second)
	second := store.open(providerForm{})
	third := store.open(providerForm{})
	if _, ok := store.get(first); ok {
		t.Fatal("oldest form kept over the limit")
	}
	if _, ok := store.get(second); !ok {
		t.Fatal("form dropped early")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := store.get(third); ok {
		t.Fatal("form kept after expiry")
	}
}

func formIDOf(t *testing.T, result Result) string {
	t.Helper()
	if result.Form == nil {
		t.Fatalf("not a form: %+v", result)
	}
	fields := strings.Fields(result.Form.SubmitCommand)
	if len(fields) != 4 || fields[1] != "form" {
		t.Fatalf("submit command = %q", result.Form.SubmitCommand)
	}
	return fields[2]
}

func resultStrings(result Result) []string {
	out := []string{result.Text}
	if result.Form != nil {
		out = append(out, result.Form.SubmitCommand, result.Form.CancelCommand)
		for _, field := range result.Form.Fields {
			out = append(out, field.Value, field.EditCommand)
		}
	}
	if result.Prompt != nil {
		out = append(out, result.Prompt.Value, result.Prompt.SubmitCommandPrefix, result.Prompt.CancelCommand, result.Prompt.Placeholder)
	}
	if result.Picker != nil {
		for _, item := range result.Picker.Items {
			out = append(out, item.Command, item.Info)
		}
	}
	return out
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// providerDaemon serves the provider setup and session LLM endpoints.
type providerDaemon struct {
	*fakeDaemon
	lastID     string
	lastUpdate setup.ProviderSetupUpdate
	configured setup.ProviderSetupItem
	providers  []setup.ProviderSetupItem
	updated    core.Session
	updateErr  error
	models     core.SessionModelsResponse
}

func newProviderDaemon(t *testing.T) *providerDaemon {
	d := &providerDaemon{fakeDaemon: newFakeDaemon(t)}
	d.on("GET /v1/setup/providers", func(*http.Request) any {
		return setup.ProviderSetupListResponse{Providers: d.providers}
	}).on("PATCH /v1/setup/providers/{id}", func(r *http.Request) any {
		d.lastID, d.lastUpdate = r.PathValue("id"), decode[setup.ProviderSetupUpdate](r)
		return setup.ProviderSetupResponse{Provider: d.configured}
	}).on("PATCH /v1/sessions/{id}/llm", func(*http.Request) any {
		if d.updateErr != nil {
			return d.updateErr
		}
		return core.SessionResponse{Session: d.updated}
	}).on("GET /v1/sessions/{id}/models", func(*http.Request) any {
		return d.models
	}).on("POST /v1/sessions/{id}/system-message", func(*http.Request) any {
		return core.MessageResponse{}
	})
	return d
}

func TestUseProviderOffersModelPickerWhenCatalogHasMultipleModels(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.updated = core.Session{ID: "session-1", ProviderID: "foresko", ModelID: "qwen3.8"}
	runtime.models = core.SessionModelsResponse{ProviderID: "foresko", ModelID: "qwen3.8", Models: []string{"gemma-4-26B-A4B", "qwen3.8"}}
	dispatcher := runtime.dispatcher(core.RoleOwner)

	result, err := dispatcher.useProvider(context.Background(), &core.Session{ID: "session-1"}, "foresko")
	if err != nil {
		t.Fatalf("useProvider: %v", err)
	}
	if result.Picker == nil {
		t.Fatal("useProvider returned no model picker")
	}
	if result.Picker.Kind != PickerSessionModels {
		t.Fatalf("picker kind = %q, want %q", result.Picker.Kind, PickerSessionModels)
	}
	if len(result.Picker.Items) != 2 {
		t.Fatalf("model count = %d, want 2", len(result.Picker.Items))
	}
	if got := result.Picker.Items[0].Command; got != "/session set-model session-1 gemma-4-26B-A4B" {
		t.Fatalf("Gemma command = %q", got)
	}
}

func TestUseProviderKeepsConfirmationWhenCatalogHasOneModel(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.updated = core.Session{ID: "session-1", ProviderID: "foresko", ModelID: "qwen3.8"}
	runtime.models = core.SessionModelsResponse{ProviderID: "foresko", ModelID: "qwen3.8", Models: []string{"qwen3.8"}}
	dispatcher := runtime.dispatcher(core.RoleOwner)

	result, err := dispatcher.useProvider(context.Background(), &core.Session{ID: "session-1"}, "foresko")
	if err != nil {
		t.Fatalf("useProvider: %v", err)
	}
	if result.Picker != nil {
		t.Fatal("single-model provider unexpectedly returned a picker")
	}
	if result.Text != "✅ Provider selected: foresko · qwen3.8" {
		t.Fatalf("confirmation = %q", result.Text)
	}
}
