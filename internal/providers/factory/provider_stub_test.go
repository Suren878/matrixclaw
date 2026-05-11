package factory

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type providerStub struct{}

func newProviderStub() *providerStub {
	return &providerStub{}
}

func (s *providerStub) Generate(context.Context, providers.Request) (providers.Response, error) {
	return providers.Response{
		Text:     "stub",
		Model:    "stub",
		Provider: "stub",
	}, nil
}
