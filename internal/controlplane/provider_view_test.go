package controlplane

import (
	"context"
	"errors"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestProviderPickerItemsSelectsConfiguredProviderWithoutEditingSetup(t *testing.T) {
	items := ProviderPickerItems([]setup.ProviderSetupItem{
		{ID: "current", Name: "Current", Configured: true},
		{ID: "custom-cloud", Name: "Custom Cloud", Configured: true},
		{ID: "new", Name: "New", Configured: false},
	}, &core.Session{ProviderID: "current", ModelID: "current-model"})

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

func TestSaveProviderEditBuildsPickerFromUpdatedSession(t *testing.T) {
	runtime := &providerRuntimeStub{
		configured: setup.ProviderSetupItem{ID: "custom-cloud", Name: "Custom Cloud", Configured: true},
		providers: []setup.ProviderSetupItem{
			{ID: "old", Name: "Old", Configured: true},
			{ID: "custom-cloud", Name: "Custom Cloud", Configured: true},
		},
		updated: core.Session{ID: "session-1", ProviderID: "custom-cloud", ModelID: "custom-model"},
	}
	dispatcher := &Dispatcher{providers: runtime}

	result, err := dispatcher.saveProviderEdit(
		context.Background(),
		&core.Session{ID: "session-1", ProviderID: "old", ModelID: "old-model"},
		runtime.configured,
		setup.ProviderFormState{Model: "custom-model"},
	)
	if err != nil {
		t.Fatalf("saveProviderEdit: %v", err)
	}
	if result.Picker == nil {
		t.Fatal("saveProviderEdit returned no provider picker")
	}

	for _, item := range result.Picker.Items {
		if item.ID == "custom-cloud" {
			if !item.Selected {
				t.Fatal("updated provider is not selected")
			}
			if item.Info != "custom-model" {
				t.Fatalf("updated provider model = %q, want custom-model", item.Info)
			}
			return
		}
	}
	t.Fatal("updated provider is missing from picker")
}

func TestSaveProviderEditReportsSessionSelectionFailure(t *testing.T) {
	runtime := &providerRuntimeStub{
		configured: setup.ProviderSetupItem{ID: "custom-cloud", Configured: true},
		updateErr:  errors.New("write failed"),
	}
	dispatcher := &Dispatcher{providers: runtime}

	_, err := dispatcher.saveProviderEdit(
		context.Background(),
		&core.Session{ID: "session-1", ProviderID: "old"},
		runtime.configured,
		setup.ProviderFormState{Model: "custom-model"},
	)
	if err == nil {
		t.Fatal("saveProviderEdit unexpectedly ignored session selection failure")
	}
}

type providerRuntimeStub struct {
	configured setup.ProviderSetupItem
	providers  []setup.ProviderSetupItem
	updated    core.Session
	updateErr  error
	models     core.SessionModelsResponse
	modelsErr  error
}

func (s *providerRuntimeStub) ListSetupProviders(context.Context) ([]setup.ProviderSetupItem, error) {
	return append([]setup.ProviderSetupItem(nil), s.providers...), nil
}

func (s *providerRuntimeStub) ConfigureSetupProvider(context.Context, string, setup.ProviderSetupUpdate) (setup.ProviderSetupItem, error) {
	return s.configured, nil
}

func (s *providerRuntimeStub) ProviderModels(context.Context, string, setup.ProviderSetupUpdate) ([]string, error) {
	return nil, nil
}

func (s *providerRuntimeStub) ProviderModelCatalog(context.Context, string, setup.ProviderSetupUpdate) (setup.ProviderModelsResponse, error) {
	return setup.ProviderModelsResponse{}, nil
}

func (s *providerRuntimeStub) DeleteSetupProvider(context.Context, string) error {
	return nil
}

func (s *providerRuntimeStub) UpdateSessionProvider(context.Context, string, string) (core.Session, error) {
	return s.updated, s.updateErr
}

func (s *providerRuntimeStub) SessionModels(context.Context, string) (core.SessionModelsResponse, error) {
	return s.models, s.modelsErr
}

func (s *providerRuntimeStub) UpdateSessionModel(context.Context, string, string) (core.Session, error) {
	return core.Session{}, nil
}

func TestUseProviderOffersModelPickerWhenCatalogHasMultipleModels(t *testing.T) {
	runtime := &providerRuntimeStub{
		updated: core.Session{ID: "session-1", ProviderID: "foresko", ModelID: "qwen3.8"},
		models: core.SessionModelsResponse{
			ProviderID: "foresko",
			ModelID:    "qwen3.8",
			Models:     []string{"gemma-4-26B-A4B", "qwen3.8"},
		},
	}
	dispatcher := &Dispatcher{providers: runtime, sessionModels: runtime}

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
	runtime := &providerRuntimeStub{
		updated: core.Session{ID: "session-1", ProviderID: "foresko", ModelID: "qwen3.8"},
		models: core.SessionModelsResponse{
			ProviderID: "foresko",
			ModelID:    "qwen3.8",
			Models:     []string{"qwen3.8"},
		},
	}
	dispatcher := &Dispatcher{providers: runtime, sessionModels: runtime}

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
