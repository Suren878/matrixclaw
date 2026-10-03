package openaicompat

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var _ providers.OutputLimiter = (*Runtime)(nil)

func TestOutputLimitsFollowConfigCatalogAndLearnedCaps(t *testing.T) {
	providers.RegisterModelMetadata("openai-limits", providers.TypeOpenAICompat, "openai-limits-model", providers.ModelMetadataRegistration{MaxOutputTokens: 6000})
	r := &Runtime{RuntimeBase: providers.RuntimeBase{Model: "openai-limits-model", MetadataID: "openai-limits", MaxOutputTokens: 4000, ProviderType: providers.TypeOpenAICompat}}
	if current, ceiling := r.OutputLimits(); current != 4000 || ceiling != 6000 {
		t.Fatalf("limits = %d/%d, want 4000/6000", current, ceiling)
	}
	r.rememberMaxTokensRejection(5000, false)
	if current, ceiling := r.OutputLimits(); current != 4000 || ceiling != 5000 {
		t.Fatalf("limits after a learned cap = %d/%d, want 4000/5000", current, ceiling)
	}
	r.rememberMaxTokensRejection(0, true)
	if current, ceiling := r.OutputLimits(); current != 0 || ceiling != 0 {
		t.Fatalf("limits without the field = %d/%d, want 0/0", current, ceiling)
	}
}
