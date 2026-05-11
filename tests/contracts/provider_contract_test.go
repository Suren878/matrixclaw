package contracts

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type ProviderFactory func(t *testing.T) providers.Runtime

func RunProviderContractTests(t *testing.T, newProvider ProviderFactory) {
	t.Helper()

	t.Run("generate reply", func(t *testing.T) {
		t.Parallel()

		runtime := newProvider(t)
		response, err := runtime.Generate(context.Background(), providers.Request{
			RunID:     "run_1",
			SessionID: "session_1",
			Messages: []providers.Message{
				{Role: "user", Content: "hello"},
			},
		})
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		if response.Text == "" {
			t.Fatalf("Generate().Text is empty")
		}
	})
}
