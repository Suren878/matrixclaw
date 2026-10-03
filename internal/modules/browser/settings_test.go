package browser

import (
	"context"
	"errors"
	"testing"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestBrowserSettingsChooseTheProviderAndRunMode(t *testing.T) {
	m := New(localruntime.New(t.TempDir()))
	if err := m.Apply(context.Background(), setup.Config{}); err != nil {
		t.Fatal(err)
	}
	items := m.Settings(context.Background())
	if engine, _ := modules.Find(items, []string{"engine"}); engine.Display != "Not Installed" || engine.Danger || engine.Confirm == "" {
		t.Fatalf("engine = %+v", engine)
	}
	if provider, _ := modules.Find(items, []string{"provider"}); provider.Value != "off" || len(provider.Options) != 2 {
		t.Fatalf("provider = %+v", provider)
	}

	cfg := setup.Config{}
	for _, change := range []struct{ key, value string }{{"provider", setup.BrowserProviderPlaywright}, {"run_mode", "always_running"}} {
		edit, err := m.Change(context.Background(), []string{change.key}, change.value)
		if err != nil {
			t.Fatal(err)
		}
		if err := edit.Config(&cfg); err != nil {
			t.Fatal(err)
		}
	}
	if browser := cfg.Modules.Browser; !browser.Enabled || browser.ProviderID != setup.BrowserProviderPlaywright || browser.ProviderConfig.RuntimeMode != "always_running" {
		t.Fatalf("browser = %+v", browser)
	}
	if _, err := m.Change(context.Background(), []string{"run_mode"}, "sometimes"); !errors.Is(err, modules.ErrInvalidSetting) {
		t.Fatalf("bad run mode error = %v", err)
	}
}
