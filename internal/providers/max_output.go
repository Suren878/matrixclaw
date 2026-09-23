package providers

// DefaultMaxOutputTokens applies when nothing more specific names an output limit.
const DefaultMaxOutputTokens int64 = 16384

// ResolveMaxOutputTokens picks the request value, then the provider config, then
// the model catalog limit when it is below the default, then the default.
func ResolveMaxOutputTokens(requested int, configured int64, providerID, providerType, modelID string) int64 {
	if requested > 0 {
		return int64(requested)
	}
	if configured > 0 {
		return configured
	}
	if limit := int64(ResolveModelMetadata(providerID, providerType, modelID).MaxOutputTokens); limit > 0 && limit < DefaultMaxOutputTokens {
		return limit
	}
	return DefaultMaxOutputTokens
}
