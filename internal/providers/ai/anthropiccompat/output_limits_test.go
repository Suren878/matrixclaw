package anthropic

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var _ providers.OutputLimiter = (*Runtime)(nil)

func TestOutputLimitsUseConfigThenDefaultUnderTheCatalogCeiling(t *testing.T) {
	providers.RegisterModelMetadata("anthropic-limits", providers.TypeAnthropic, "claude-limits-model", providers.ModelMetadataRegistration{MaxOutputTokens: 64000})
	for _, tc := range []struct {
		configured, current int64
	}{
		{2000, 2000},
		{0, providers.DefaultMaxOutputTokens},
	} {
		r := &Runtime{model: "claude-limits-model", providerID: "anthropic-limits", maxTokens: tc.configured}
		if current, ceiling := r.OutputLimits(); current != tc.current || ceiling != 64000 {
			t.Errorf("configured %d: limits = %d/%d, want %d/64000", tc.configured, current, ceiling, tc.current)
		}
	}
}
