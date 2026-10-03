package gateway

import (
	"strings"
	"testing"
	"time"
)

// The environment of the deployed gateway unit; each variable keeps its meaning.
func TestConfigFromEnvKeepsTheDeployedContract(t *testing.T) {
	env := map[string]string{
		"MATRIXCLAW_API_URL":                           "http://127.0.0.1:8081/",
		"MATRIXCLAW_TELEPHONY_ADDR":                    "127.0.0.1:8095",
		"MATRIXCLAW_TELEPHONY_ARI_APP":                 "claw",
		"MATRIXCLAW_TELEPHONY_ARI_URL":                 "http://127.0.0.1:18089/ari/",
		"MATRIXCLAW_TELEPHONY_ARI_USER":                "ari-user",
		"MATRIXCLAW_TELEPHONY_CALLER_ID":               "+15550001111",
		"MATRIXCLAW_TELEPHONY_RTP_BIND":                "0.0.0.0:41000",
		"MATRIXCLAW_TELEPHONY_SIP_PROFILE":             "trunk",
		"MATRIXCLAW_TELEPHONY_INBOUND_ENABLED":         "1",
		"MATRIXCLAW_TELEPHONY_INBOUND_ALLOWED_CALLERS": "+15550002222, +442012345678",
		"MATRIXCLAW_TELEPHONY_INBOUND_GREETING":        "Hello there.",
		"MATRIXCLAW_TELEPHONY_INBOUND_PROMPT":          "Take a message for the owner.",
		"MATRIXCLAW_TELEPHONY_TOKEN":                   "gw-token",
		"MATRIXCLAW_API_TOKEN":                         "api-token",
		"MATRIXCLAW_TELEPHONY_ARI_PASSWORD":            "ari-pass",
		"MATRIXCLAW_TELEPHONY_CALL_TIMEOUT":            "30s",
		"MATRIXCLAW_TELEPHONY_MAX_CALL_DURATION":       "120",
	}
	for name, value := range env {
		t.Setenv(name, value)
	}
	cfg := ConfigFromEnv()
	checks := map[string][2]string{
		"MatrixclawURL":   {cfg.MatrixclawURL, "http://127.0.0.1:8081"},
		"HTTPAddr":        {cfg.HTTPAddr, "127.0.0.1:8095"},
		"ARIApp":          {cfg.ARIApp, "claw"},
		"ARIURL":          {cfg.ARIURL, "http://127.0.0.1:18089/ari"},
		"ARIUser":         {cfg.ARIUser, "ari-user"},
		"CallerID":        {cfg.CallerID, "+15550001111"},
		"RTPBind":         {cfg.RTPBind, "0.0.0.0:41000"},
		"SIPProfile":      {cfg.SIPProfile, "trunk"},
		"InboundGreeting": {cfg.InboundGreeting, "Hello there."},
		"InboundPrompt":   {cfg.InboundPrompt, "Take a message for the owner."},
		"GatewayToken":    {cfg.GatewayToken, "gw-token"},
		"MatrixclawToken": {cfg.MatrixclawToken, "api-token"},
		"ARIPassword":     {cfg.ARIPassword, "ari-pass"},
	}
	for field, check := range checks {
		if check[0] != check[1] {
			t.Errorf("%s = %q, want %q", field, check[0], check[1])
		}
	}
	if !cfg.InboundEnabled || cfg.CallTimeout != 30*time.Second || cfg.MaxCallDuration != 2*time.Minute {
		t.Fatalf("inbound %v timeout %s max %s", cfg.InboundEnabled, cfg.CallTimeout, cfg.MaxCallDuration)
	}
	if !cfg.InboundCallerAllowed("+15550002222") || !cfg.InboundCallerAllowed("+442012345678") || cfg.InboundCallerAllowed("+15559999999") {
		t.Fatalf("allowlist = %v", cfg.InboundAllowed)
	}
}

func TestInboundCallersAreDeniedWithoutAnAllowlist(t *testing.T) {
	if (Config{}).InboundCallerAllowed("+15550002222") {
		t.Fatal("caller admitted without an allowlist")
	}
}

func TestInboundInstructionsKeepTheConfiguredPromptAndGreeting(t *testing.T) {
	instruction := phoneSystemInstruction(phonePromptInput{
		CallID:        "call_1",
		OpeningPhrase: "Hello there.",
		Objective:     inboundSystemInstruction("Take a message for the owner."),
		Direction:     "inbound",
	})
	for _, want := range []string{"Hello there.", "Take a message for the owner.", "call_1", "inbound"} {
		if !strings.Contains(instruction, want) {
			t.Errorf("instruction lacks %q", want)
		}
	}
}
