package agentcontext

import (
	"fmt"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// stopReasonError fails a summary cut by the output limit or a content filter.
func stopReasonError(response providers.Response) error {
	switch response.StopReason {
	case providers.StopMaxTokens, providers.StopContentFilter:
		return fmt.Errorf("%s: generation stopped before completion (%s)", response.Provider, response.StopReason)
	default:
		return nil
	}
}
