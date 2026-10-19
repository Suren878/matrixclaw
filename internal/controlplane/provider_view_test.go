package controlplane

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestProviderPickerItemsOpenTheModelsOfEveryConfiguredProvider(t *testing.T) {
	providers := []setup.ProviderSetupItem{
		{ID: "current", Name: "Current", Model: "default-model", Configured: true},
		{ID: "custom-cloud", Name: "Custom Cloud", Model: "cloud-model", Configured: true},
		{ID: "new", Name: "New", Configured: false},
	}
	session := core.Session{ID: "s1", ProviderID: "current", ModelID: "current-model"}

	items := map[string]PickerItem{}
	for _, item := range providerPickerItems(providers, session, true) {
		items[item.ID] = item
	}
	if got := items["current"]; got.Command != "/provider models s1 current" || !got.Selected || got.Info != "current-model" {
		t.Fatalf("current provider = %+v, want its models with the session's model", got)
	}
	if got := items["custom-cloud"]; got.Command != "/provider models s1 custom-cloud" || got.Selected || got.Info != "cloud-model" {
		t.Fatalf("configured provider = %+v, want its models", got)
	}
	if got := items["new"].Command; got != "/provider edit new" {
		t.Fatalf("unconfigured provider command = %q, want setup command", got)
	}
	if got := items["default"].Command; got != "/provider default" {
		t.Fatalf("default for new sessions command = %q", got)
	}

	for _, item := range providerPickerItems(providers, session, false) {
		if item.ID == "new" || item.ID == "default" || item.ID == "custom" {
			t.Fatalf("a member sees %q", item.ID)
		}
	}
}

func TestProviderModelsSetProviderAndModelTogether(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.sessions = []core.Session{{ID: "s1", ProviderID: "openai", ModelID: "gpt-5"}}
	runtime.models = core.SessionModelsResponse{ProviderID: "foresko", ModelID: "qwen3.8", Models: []string{"gemma-4-26B-A4B", "qwen3.8"}}
	runtime.updated = core.Session{ID: "s1", ProviderID: "foresko", ModelID: "gemma-4-26B-A4B"}

	picker := screenOf[PickerData](t, runtime.run("/provider models s1 foresko"))
	if runtime.modelsProvider != "foresko" || picker.Back != "/session model s1" {
		t.Fatalf("models of %q, back %q", runtime.modelsProvider, picker.Back)
	}
	if len(picker.Items) != 3 || picker.Items[0].Command != "/provider use s1 foresko gemma-4-26B-A4B" || picker.Items[1].Selected || picker.Items[2].Command != "/provider edit foresko" {
		t.Fatalf("model picker = %+v", picker.Items)
	}

	result := runtime.run(picker.Items[0].Command)
	if runtime.llm != (core.UpdateSessionLLMRequest{ProviderID: "foresko", ModelID: "gemma-4-26B-A4B"}) {
		t.Fatalf("PATCH llm = %+v", runtime.llm)
	}
	if result.Text() != "✅ Model selected: foresko · gemma-4-26B-A4B" || runtime.called("POST /v1/sessions/s1/system-message") != 1 {
		t.Fatalf("confirmation = %q", result.Text())
	}
}

func TestModelPickerTicksTheSessionModelAndOffersOneModel(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.sessions = []core.Session{{ID: "s1", ProviderID: "foresko", ModelID: "qwen3.8"}}
	runtime.providers = []setup.ProviderSetupItem{{ID: "foresko", Name: "Foresko", Configured: true}}
	runtime.models = core.SessionModelsResponse{ProviderID: "foresko", ModelID: "qwen3.8", Models: []string{"qwen3.8"}}

	picker := screenOf[PickerData](t, runtime.runAs(core.RoleMember, "/provider foresko"))
	if len(picker.Items) != 1 || !picker.Items[0].Selected || picker.Items[0].Command != "/provider use s1 foresko qwen3.8" {
		t.Fatalf("member's one-model picker = %+v", picker.Items)
	}
}

func TestModelPickerFallsBackToTypingWhenDiscoveryFails(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.models = core.SessionModelsResponse{ProviderID: "local", ModelID: "llama", ModelsError: "connection refused"}
	runtime.updated = core.Session{ID: "s1", ProviderID: "local", ModelID: "llama-70b"}

	picker := screenOf[PickerData](t, runtime.run("/provider models s1 local"))
	if picker.Meta != "connection refused" || len(picker.Items) != 3 || picker.Items[0].Command != "/provider use s1 local llama" {
		t.Fatalf("fallback picker = %+v", picker)
	}
	prompt := runtime.run(picker.Items[1].Command)
	runtime.answer(prompt, "llama-70b")
	if runtime.llm != (core.UpdateSessionLLMRequest{ProviderID: "local", ModelID: "llama-70b"}) {
		t.Fatalf("typed model sent %+v", runtime.llm)
	}
}

func TestSavedProviderFormLeavesTheSessionAlone(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.sessions = []core.Session{{ID: "s1", ProviderID: "old", ModelID: "old-model"}}
	runtime.configured = setup.ProviderSetupItem{ID: "openai", Name: "OpenAI", Configured: true}
	runtime.providers = []setup.ProviderSetupItem{
		{ID: "old", Name: "Old", Configured: true, Active: true, Model: "old-model"},
		{ID: "openai", Name: "OpenAI", Type: "openai", Model: "gpt-5", APIKeyPreview: "****1234", Configured: true},
	}
	form := screenOf[FormData](t, runtime.run("/provider edit openai"))
	for _, field := range form.Fields {
		if field.ID == "model" {
			t.Fatalf("the provider form has a model field: %+v", form.Fields)
		}
	}

	result := runtime.run(form.SubmitCommand)

	if runtime.called("PATCH /v1/sessions/s1/llm") != 0 || runtime.lastUpdate.Active || runtime.lastUpdate.Model != nil {
		t.Fatalf("saving switched the session or the default: %+v", runtime.lastUpdate)
	}
	picker := screenOf[PickerData](t, result)
	if picker.Meta != "Default for new sessions: Old · old-model" || !picker.Items[0].Selected || picker.Items[0].ID != "old" {
		t.Fatalf("provider list after save = %+v", picker)
	}
}

func TestDefaultForNewSessionsSetsTheActiveProviderAndModel(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.providers = []setup.ProviderSetupItem{
		{ID: "old", Name: "Old", Configured: true, Active: true, Model: "old-model"},
		{ID: "openai", Name: "OpenAI", Model: "gpt-5", Configured: true},
		{ID: "new", Name: "New"},
	}
	runtime.catalog = setup.ProviderModelsResponse{Status: setup.ProviderModelStatusOK, Models: []string{"gpt-5", "gpt-5-mini"}}
	runtime.configured = setup.ProviderSetupItem{ID: "openai", Name: "OpenAI", Model: "gpt-5-mini", Configured: true, Active: true}

	list := screenOf[PickerData](t, runtime.run("/provider default"))
	if len(list.Items) != 2 || !list.Items[0].Selected || list.Items[1].Command != "/provider default openai" {
		t.Fatalf("default providers = %+v", list.Items)
	}
	models := screenOf[PickerData](t, runtime.run(list.Items[1].Command))
	if models.Items[1].Command != "/provider default-use openai gpt-5-mini" || models.Items[0].Selected {
		t.Fatalf("default models = %+v", models.Items)
	}

	result := runtime.run(models.Items[1].Command)

	if runtime.lastID != "openai" || deref(runtime.lastUpdate.Model) != "gpt-5-mini" || !runtime.lastUpdate.Active {
		t.Fatalf("saved %q %+v", runtime.lastID, runtime.lastUpdate)
	}
	if result.Text() != "✅ Default for new sessions: OpenAI · gpt-5-mini" || runtime.called("PATCH /v1/sessions/s1/llm") != 0 {
		t.Fatalf("result = %q", result.Text())
	}
	if got := runtime.runAs(core.RoleMember, "/provider default").Text(); got != ownerOnlySettings {
		t.Fatalf("member default = %q", got)
	}
}

func TestProviderFormKeepsTheKeyOutOfCommands(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.configured = setup.ProviderSetupItem{ID: "local-ai", Configured: true}
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
	if runtime.lastID != "local-ai" || deref(update.APIKey) != "sk-secret-1234" || deref(update.Name) != "Local AI" || deref(update.Model) != "qwen" || deref(update.Type) != "openai-compatible" || update.Active {
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
	lastID         string
	lastUpdate     setup.ProviderSetupUpdate
	configured     setup.ProviderSetupItem
	providers      []setup.ProviderSetupItem
	catalog        setup.ProviderModelsResponse
	updated        core.Session
	llm            core.UpdateSessionLLMRequest
	models         core.SessionModelsResponse
	modelsProvider string
}

func newProviderDaemon(t *testing.T) *providerDaemon {
	d := &providerDaemon{fakeDaemon: newFakeDaemon(t)}
	d.on("GET /v1/setup/providers", func(*http.Request) any {
		return setup.ProviderSetupListResponse{Providers: d.providers}
	}).on("PATCH /v1/setup/providers/{id}", func(r *http.Request) any {
		d.lastID, d.lastUpdate = r.PathValue("id"), decode[setup.ProviderSetupUpdate](r)
		return setup.ProviderSetupResponse{Provider: d.configured}
	}).on("POST /v1/setup/providers/{id}/models", func(*http.Request) any {
		return d.catalog
	}).on("PATCH /v1/sessions/{id}/llm", func(r *http.Request) any {
		d.llm = decode[core.UpdateSessionLLMRequest](r)
		return core.SessionResponse{Session: d.updated}
	}).on("GET /v1/sessions/{id}/models", func(r *http.Request) any {
		d.modelsProvider = r.URL.Query().Get("provider")
		return d.models
	}).on("POST /v1/sessions/{id}/system-message", func(*http.Request) any {
		return core.MessageResponse{}
	})
	return d
}

func TestSessionMenuModelOpensTheProviderList(t *testing.T) {
	runtime := newProviderDaemon(t)
	runtime.sessions = []core.Session{{ID: "s1", ProviderID: "openai", ModelID: "gpt-5"}}
	runtime.providers = []setup.ProviderSetupItem{{ID: "openai", Name: "OpenAI", Configured: true}}

	picker := screenOf[PickerData](t, runtime.runAs(core.RoleMember, "/session model s1"))
	if picker.Kind != PickerProvider || picker.Back != "/session menu s1" || picker.Items[0].Command != "/provider models s1 openai" {
		t.Fatalf("session model row = %+v", picker)
	}
}
