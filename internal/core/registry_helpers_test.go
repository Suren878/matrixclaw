package core_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func newCoreCodingRegistry() *tools.Registry {
	return tools.NewCoreCodingRegistry()
}

type sessionLLMRegistryStub struct {
	runtime       providers.Runtime
	providerID    string
	providerType  string
	modelID       string
	contextWindow int
}

func newSessionLLMRegistryStub(runtime providers.Runtime) *sessionLLMRegistryStub {
	return &sessionLLMRegistryStub{runtime: runtime}
}

func (s *sessionLLMRegistryStub) ActiveSelection() (string, string) {
	return s.providerIDValue(), s.modelIDValue()
}

func (s *sessionLLMRegistryStub) Providers() []core.SessionProviderOption {
	return []core.SessionProviderOption{s.option()}
}

func (s *sessionLLMRegistryStub) Normalize(providerID string, modelID string) (core.SessionProviderOption, string, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		providerID, _ = s.ActiveSelection()
	}
	if providerID != s.providerIDValue() {
		return core.SessionProviderOption{}, "", fmt.Errorf("provider %q is not configured", providerID)
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		_, modelID = s.ActiveSelection()
	}
	return s.option(), modelID, nil
}

func (s *sessionLLMRegistryStub) Models(context.Context, string) ([]string, error) {
	return []string{s.modelIDValue()}, nil
}

func (s *sessionLLMRegistryStub) Resolve(ctx context.Context, providerID string, modelID string) (providers.Runtime, core.SessionProviderOption, string, error) {
	option, resolvedModel, err := s.Normalize(providerID, modelID)
	if err != nil {
		return nil, core.SessionProviderOption{}, "", err
	}
	return s.runtime, option, resolvedModel, nil
}

func (s *sessionLLMRegistryStub) ContextWindowTokens(string, string) (int, bool) {
	if s.contextWindow <= 0 {
		return 0, false
	}
	return s.contextWindow, true
}

func (s *sessionLLMRegistryStub) option() core.SessionProviderOption {
	return core.SessionProviderOption{
		ID:           s.providerIDValue(),
		Label:        "Stub Provider",
		Type:         s.providerTypeValue(),
		DefaultModel: s.modelIDValue(),
		Configured:   true,
	}
}

func (s *sessionLLMRegistryStub) providerIDValue() string {
	if value := strings.TrimSpace(s.providerID); value != "" {
		return value
	}
	return "stub-provider"
}

func (s *sessionLLMRegistryStub) providerTypeValue() string {
	if value := strings.TrimSpace(s.providerType); value != "" {
		return value
	}
	return "stub"
}

func (s *sessionLLMRegistryStub) modelIDValue() string {
	if value := strings.TrimSpace(s.modelID); value != "" {
		return value
	}
	return "stub-model"
}
