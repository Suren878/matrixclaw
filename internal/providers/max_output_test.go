package providers

import "testing"

func TestResolveMaxOutputTokensOrder(t *testing.T) {
	RegisterModelMetadata("maxout-test", TypeOpenAICompat, "small-output-model", ModelMetadataRegistration{MaxOutputTokens: 8192})
	RegisterModelMetadata("maxout-test", TypeOpenAICompat, "large-output-model", ModelMetadataRegistration{MaxOutputTokens: 128000})
	for _, tc := range []struct {
		name       string
		requested  int
		configured int64
		model      string
		want       int64
	}{
		{"request wins", 40000, 2048, "small-output-model", 40000},
		{"config before catalog", 0, 2048, "small-output-model", 2048},
		{"catalog below default", 0, 0, "small-output-model", 8192},
		{"catalog above default is capped", 0, 0, "large-output-model", DefaultMaxOutputTokens},
		{"unknown model", 0, 0, "unlisted-model", DefaultMaxOutputTokens},
	} {
		if got := ResolveMaxOutputTokens(tc.requested, tc.configured, "maxout-test", TypeOpenAICompat, tc.model); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := ResolveModelMetadata("maxout-test", TypeOpenAICompat, "large-output-model").MaxOutputTokens; got != 128000 {
		t.Errorf("catalog metadata MaxOutputTokens = %d, want 128000", got)
	}
}
