package providers

import "testing"

func TestAnthropicProfilesUseNativeToolCalling(t *testing.T) {
	for _, tc := range []struct{ providerID, modelID string }{
		{"anthropic", "claude-sonnet-4-5"},
		{"custom-anthropic-compatible", "glm-4.6"},
		{"", ""},
	} {
		profile := ProfileForModel(tc.providerID, TypeAnthropic, tc.modelID)
		if profile.RuntimeProfile.ToolUseMode != ToolUseNative || !profile.Capabilities.ToolCalling {
			t.Errorf("%q/%q: tool use mode = %q, tool calling = %t", tc.providerID, tc.modelID, profile.RuntimeProfile.ToolUseMode, profile.Capabilities.ToolCalling)
		}
		if profile.RuntimeProfile.ToolSchemaDialect != ToolSchemaJSONSchema {
			t.Errorf("%q/%q: schema dialect = %q", tc.providerID, tc.modelID, profile.RuntimeProfile.ToolSchemaDialect)
		}
	}
}
