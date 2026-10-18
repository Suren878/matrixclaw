package controlplane

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

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
	if got := commands["current"]; got != "/provider edit current" {
		t.Fatalf("current provider command = %q, want edit command", got)
	}
	if got := commands["custom-cloud"]; got != "/provider use custom-cloud" {
		t.Fatalf("configured provider command = %q, want session selection command", got)
	}
	if got := commands["new"]; got != "/provider edit new" {
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
	runtime.updated = core.Session{ID: "s1", ProviderID: "custom-cloud", ModelID: "custom-model"}
	form := runtime.run("/provider edit custom-cloud")

	result := runtime.run(screenOf[FormData](t, form).SubmitCommand)

	for _, item := range screenOf[PickerData](t, result).Items {
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
	runtime.providers = []setup.ProviderSetupItem{{ID: "custom-cloud", Type: "openai-compatible", Name: "Custom Cloud", BaseURL: "http://x", Model: "m", APIKeyPreview: "****1234", Configured: true}}
	runtime.updateErr = errors.New("write failed")
	form := runtime.run("/provider edit custom-cloud")

	if _, err := runtime.dispatcher(core.RoleOwner).Handle(context.Background(), screenOf[FormData](t, form).SubmitCommand); err == nil {
		t.Fatal("session selection failure was ignored")
	}
}

func TestProviderFormKeepsTheKeyOutOfCommands(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.configured = setup.ProviderSetupItem{ID: "local-ai", Configured: true}
	runtime.updated = core.Session{ID: "s1", ProviderID: "local-ai"}
	results := []Result{runtime.run("/provider custom new openai")}
	formID := formIDOf(t, results[0])
	commands := []string{}
	edit := func(field string) Result {
		command := providerFormEdit.command(formID, field)
		commands = append(commands, command)
		return runtime.run(command)
	}
	for _, step := range [][2]string{{"key", "sk-secret-1234"}, {"name", "Local AI"}, {"base", "http://127.0.0.1:8000/v1"}, {"model", "qwen"}} {
		prompt := edit(step[0])
		commands = append(commands, screenOf[PromptData](t, prompt).Submit)
		results = append(results, prompt, runtime.answer(prompt, step[1]))
	}
	results = append(results, edit("key"))
	for _, result := range results {
		commands = append(commands, resultStrings(result)...)
	}
	for _, text := range commands {
		if strings.Contains(text, "sk-secret") {
			t.Fatalf("a command or screen carries the key: %q", text)
		}
	}
	if prompt := screenOf[PromptData](t, results[1]); !prompt.Sensitive {
		t.Fatalf("key prompt = %+v, want sensitive", prompt)
	}

	runtime.run(screenOf[FormData](t, results[len(results)-2]).SubmitCommand)
	update := runtime.lastUpdate
	if runtime.lastID != "local-ai" || deref(update.APIKey) != "sk-secret-1234" || deref(update.Name) != "Local AI" || deref(update.Model) != "qwen" || deref(update.Type) != "openai-compatible" || !update.Active {
		t.Fatalf("saved %q %+v", runtime.lastID, update)
	}
	if expired := runtime.run(providerFormShow.command(formID)); !strings.Contains(expired.Text(), "expired") {
		t.Fatalf("form still open after save: %+v", expired)
	}
}

func TestProviderFormsBelongToTheirDispatcher(t *testing.T) {
	runtime := newProviderDaemon(t)
	form := runtime.run("/provider custom new openai")

	other := New(runtime.dispatcher(core.RoleOwner).daemon, "")
	result, err := other.Handle(context.Background(), screenOf[FormData](t, form).SubmitCommand)
	if err != nil || !strings.Contains(result.Text(), "expired") {
		t.Fatalf("another dispatcher opened the form: %+v, %v", result, err)
	}
}

func formIDOf(t *testing.T, result Result) string {
	t.Helper()
	route, ok := DecodeRoute(screenOf[FormData](t, result).SubmitCommand)
	if !ok || !route.Is(providerFormSave) {
		t.Fatalf("submit command = %q", screenOf[FormData](t, result).SubmitCommand)
	}
	return route.arg(0)
}

func resultStrings(result Result) []string {
	out := []string{result.Text()}
	switch screen := result.Screen.(type) {
	case FormData:
		out = append(out, screen.SubmitCommand, screen.CancelCommand)
		for _, field := range screen.Fields {
			out = append(out, field.Value, field.EditCommand)
		}
	case PromptData:
		out = append(out, screen.Value, screen.Submit, screen.CancelCommand, screen.Placeholder)
	case PickerData:
		for _, item := range screen.Items {
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
	if !isScreen[PickerData](result) {
		t.Fatal("useProvider returned no model picker")
	}
	if screenOf[PickerData](t, result).Kind != PickerSessionModels {
		t.Fatalf("picker kind = %q, want %q", screenOf[PickerData](t, result).Kind, PickerSessionModels)
	}
	if len(screenOf[PickerData](t, result).Items) != 2 {
		t.Fatalf("model count = %d, want 2", len(screenOf[PickerData](t, result).Items))
	}
	if got := screenOf[PickerData](t, result).Items[0].Command; got != "/session set-model session-1 gemma-4-26B-A4B" {
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
	if isScreen[PickerData](result) {
		t.Fatal("single-model provider unexpectedly returned a picker")
	}
	if result.Text() != "✅ Provider selected: foresko · qwen3.8" {
		t.Fatalf("confirmation = %q", result.Text())
	}
}
