package webtools

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/webresearch"
)

type WebService struct {
	config func() (WebSearchProviderConfig, error)
	engine *webresearch.Engine
}

func NewWebService(config func() (WebSearchProviderConfig, error), engine *webresearch.Engine) *WebService {
	return &WebService{
		config: config,
		engine: engine,
	}
}

func (s *WebService) Search(ctx context.Context, query string, limit int) ([]WebSearchResult, string, error) {
	var cfg WebSearchProviderConfig
	if s != nil && s.config != nil {
		var err error
		cfg, err = s.config()
		if err != nil {
			return nil, "", err
		}
	}
	return RunWebSearch(ctx, query, limit, cfg)
}

func (s *WebService) ResearchConfigured() bool {
	return s != nil && s.engine != nil
}

func (s *WebService) Research(ctx context.Context, request webresearch.ResearchRequest) (webresearch.ResearchResult, error) {
	if !s.ResearchConfigured() {
		return webresearch.ResearchResult{}, fmt.Errorf("web research engine is not configured")
	}
	return s.engine.Research(ctx, request)
}

func (s *WebService) Ask(ctx context.Context, request webresearch.AskRequest) (webresearch.ResearchResult, error) {
	if !s.ResearchConfigured() {
		return webresearch.ResearchResult{}, fmt.Errorf("web research engine is not configured")
	}
	return s.engine.Ask(ctx, request)
}

func (s *WebService) Status(ctx context.Context, request webresearch.StatusRequest) (webresearch.ResearchResult, error) {
	if !s.ResearchConfigured() {
		return webresearch.ResearchResult{}, fmt.Errorf("web research engine is not configured")
	}
	return s.engine.Status(ctx, request)
}
