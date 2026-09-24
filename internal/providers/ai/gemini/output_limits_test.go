package gemini

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var _ providers.OutputLimiter = (*Runtime)(nil)

func TestOutputLimitsUseConfigThenDefaultUnderTheCatalogCeiling(t *testing.T) {
	providers.RegisterModelMetadata("gemini-limits", providers.TypeGemini, "gemini-limits-model", providers.ModelMetadataRegistration{MaxOutputTokens: 65536})
	for _, tc := range []struct {
		configured, current int64
	}{
		{3000, 3000},
		{0, providers.DefaultMaxOutputTokens},
	} {
		r := &Runtime{model: "gemini-limits-model", providerID: "gemini-limits", maxOutputTokens: tc.configured}
		if current, ceiling := r.OutputLimits(); current != tc.current || ceiling != 65536 {
			t.Errorf("configured %d: limits = %d/%d, want %d/65536", tc.configured, current, ceiling, tc.current)
		}
	}
}
