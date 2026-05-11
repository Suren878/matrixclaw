package integration

import (
	"context"
	"errors"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type providerStub struct {
	GenerateFunc func(ctx context.Context, request providers.Request) (providers.Response, error)
}

func newProviderStub() *providerStub {
	return &providerStub{}
}

func (s *providerStub) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	if s.GenerateFunc != nil {
		return s.GenerateFunc(ctx, request)
	}

	lastUser := ""
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == "user" {
			lastUser = strings.TrimSpace(request.Messages[i].Content)
			break
		}
	}
	if lastUser == "" {
		return providers.Response{}, errors.New("provider stub: no user message")
	}

	return providers.Response{
		Text:     "Stub reply: " + lastUser,
		Model:    "stub",
		Provider: "stub",
	}, nil
}
