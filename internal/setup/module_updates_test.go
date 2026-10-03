package setup

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTelephonyUpdateChangesOnlyPresentFields(t *testing.T) {
	service, _ := testService(t, Config{Modules: ModulesConfig{Telephony: TelephonyConfig{GatewayURL: "http://gw:8090", GatewayToken: "gw-secret", DefaultProfile: "main"}}})
	module, err := service.UpdateTelephonyModule(TelephonyModuleUpdate{PhonePrompt: ptr("Be polite.")})
	if err != nil {
		t.Fatal(err)
	}
	if !module.TokenConfigured || module.DefaultProfile != "main" || module.Config.PhonePrompt != "Be polite." {
		t.Fatalf("module = %+v", module)
	}
	if module, err = service.UpdateTelephonyModule(TelephonyModuleUpdate{GatewayToken: ptr(""), DefaultProfile: ptr("")}); err != nil {
		t.Fatal(err)
	}
	if module.TokenConfigured || module.DefaultProfile != "" || module.GatewayURL != "http://gw:8090" {
		t.Fatalf("module after clearing = %+v", module)
	}
}

func TestWebSearchResponseShowsNoKeys(t *testing.T) {
	service, _ := testService(t, Config{})
	cfg, err := service.UpdateWebSearchConfig(WebSearchConfigUpdate{Provider: ptr(WebSearchProviderTavily), TavilyKey: ptr("tvly-secret-1234")})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TavilyKey != "tvly-secret-1234" {
		t.Fatalf("stored key = %q", cfg.TavilyKey)
	}
	data, _ := json.Marshal(WebSearchResponse(cfg))
	if strings.Contains(string(data), "secret") || !strings.Contains(string(data), "****1234") {
		t.Fatalf("response = %s", data)
	}
	if _, err := service.UpdateWebSearchConfig(WebSearchConfigUpdate{TavilyKey: ptr("")}); err == nil {
		t.Fatal("cleared the key Tavily needs")
	}
	if cfg, err = service.UpdateWebSearchConfig(WebSearchConfigUpdate{Provider: ptr(WebSearchProviderDDG), TavilyKey: ptr("")}); err != nil || cfg.TavilyKey != "" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}
