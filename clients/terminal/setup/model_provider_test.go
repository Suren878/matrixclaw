package setup

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestProviderSaveNeedsAKeyAndStoresOnlyUserValues(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	m := &model{cfg: setup.NewConfig(), screen: screenProviderForm}
	m.editingProvider = setup.ProviderConfig{ID: "openai"}

	if err := m.handleProviderFormSave(); err == nil {
		t.Fatal("saved a provider without an API key")
	}
	m.editingProvider.APIKey = "sk-test"
	if err := m.handleProviderFormSave(); err != nil {
		t.Fatal(err)
	}
	saved, ok := m.cfg.Provider("openai")
	if !ok || m.cfg.ActiveProviderID != "openai" {
		t.Fatalf("providers = %+v, active %q", m.cfg.Providers, m.cfg.ActiveProviderID)
	}
	if saved.Model != "" || saved.BaseURL != "" || saved.APIKey != "sk-test" {
		t.Fatalf("saved = %+v, want only the key", saved)
	}
	if saved.Effective().Model == "" {
		t.Fatal("no default model")
	}
}

func TestBoolPickerEditsTheTypedConfig(t *testing.T) {
	m := &model{cfg: setup.NewConfig()}
	m.openBoolPicker(boolEditTelegramEnabled, m.cfg.Clients.Telegram.Enabled)
	_, _ = m.updateBoolPicker(keyPress(tea.KeyUp, 0))
	_, _ = m.updateBoolPicker(keyPress(tea.KeyEnter, 0))
	if !m.cfg.Clients.Telegram.Enabled || m.screen != screenTelegramForm {
		t.Fatalf("enabled=%t screen=%v", m.cfg.Clients.Telegram.Enabled, m.screen)
	}
}

func TestModelSearchStaysInTheModelPicker(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	m := &model{cfg: setup.NewConfig(), screen: screenProviderList, filterInput: newSearchField("Find a provider")}
	for _, r := range "open" {
		_, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	providers := len(m.providerEntries())
	m.editingProvider = setup.ProviderConfig{ID: "openai", APIKey: "sk-test"}
	m.openForm(screenProviderForm)
	_ = m.openProviderModelPicker(context.Background())
	_, _ = m.Update(providerModelsLoadedMsg{seq: m.providerModelLoadSeq, response: setup.ProviderModelsResponse{Status: setup.ProviderModelStatusOK, Models: []string{"gpt-zzz-1", "gpt-4o"}}})
	if rows := len(m.providerModelRows()); rows != 2 {
		t.Fatalf("model picker shows %d of 2 models", rows)
	}
	for _, r := range "zzz" {
		_, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.editingProvider.Model != "gpt-zzz-1" {
		t.Fatalf("picked model %q", m.editingProvider.Model)
	}
	if err := m.handleProviderFormSave(); err != nil {
		t.Fatal(err)
	}
	if got := len(m.providerEntries()); got != providers {
		t.Fatalf("provider list shows %d entries after picking a model, want %d", got, providers)
	}
}
