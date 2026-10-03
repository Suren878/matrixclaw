package setup

import (
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
