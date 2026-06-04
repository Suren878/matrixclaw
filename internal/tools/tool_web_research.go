package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/webresearch"
)

type webResearchExecutor struct {
	name string
	web  *WebService
}

func NewWebResearchExecutors(engine *webresearch.Engine) []Executor {
	return NewWebResearchExecutorsWithService(NewWebService(nil, engine))
}

func NewWebResearchExecutorsWithService(web *WebService) []Executor {
	return []Executor{
		&webResearchExecutor{name: webResearchToolName, web: web},
		&webResearchExecutor{name: webResearchAskToolName, web: web},
		&webResearchExecutor{name: webResearchStatusToolName, web: web},
	}
}

func (e *webResearchExecutor) Spec() Spec {
	switch e.name {
	case webResearchAskToolName:
		return Spec{
			ID:              webResearchAskToolName,
			Name:            "WebResearchAsk",
			Description:     "Ask a follow-up question against a previous web_research session; reuses stored facts/artifacts before refetching",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreWeb,
			Category:        CategoryWeb,
			Profiles:        []Profile{ProfileCoding, ProfileWeb},
			OutputKind:      OutputSearchResults,
			InputJSONSchema: webResearchAskInputSchema,
		}
	case webResearchStatusToolName:
		return Spec{
			ID:              webResearchStatusToolName,
			Name:            "WebResearchStatus",
			Description:     "Check status and compact results for a web_research session by research_id",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreWeb,
			Category:        CategoryWeb,
			Profiles:        []Profile{ProfileCoding, ProfileWeb},
			OutputKind:      OutputJob,
			InputJSONSchema: webResearchStatusInputSchema,
		}
	default:
		return Spec{
			ID:              webResearchToolName,
			Name:            "WebResearch",
			Description:     "Research the web using search/fetch/browser fallback and return only compact facts, sources, warnings, next actions, and a research_id",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreWeb,
			Category:        CategoryWeb,
			Profiles:        []Profile{ProfileCoding, ProfileWeb},
			OutputKind:      OutputSearchResults,
			InputJSONSchema: webResearchInputSchema,
		}
	}
}

func (e *webResearchExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	if e == nil || e.web == nil || !e.web.ResearchConfigured() {
		return Result{Content: "web research engine is not configured", Status: ResultStatusError, IsError: true}, nil
	}
	var result webresearch.ResearchResult
	var err error
	switch e.name {
	case webResearchAskToolName:
		var input webresearch.AskRequest
		if decodeErr := json.Unmarshal(call.Args, &input); decodeErr != nil {
			return Result{}, InvalidArgs(webResearchAskToolName, decodeErr)
		}
		result, err = e.web.Ask(ctx, input)
	case webResearchStatusToolName:
		var input webresearch.StatusRequest
		if decodeErr := json.Unmarshal(call.Args, &input); decodeErr != nil {
			return Result{}, InvalidArgs(webResearchStatusToolName, decodeErr)
		}
		result, err = e.web.Status(ctx, input)
	default:
		var input webresearch.ResearchRequest
		if decodeErr := json.Unmarshal(call.Args, &input); decodeErr != nil {
			return Result{}, InvalidArgs(webResearchToolName, decodeErr)
		}
		result, err = e.web.Research(ctx, input)
	}
	if err != nil {
		return Result{
			Content: fmt.Sprintf("%s failed: %v", e.name, err),
			Status:  ResultStatusError,
			IsError: true,
		}, nil
	}
	status := ResultStatusSuccess
	isError := false
	switch result.Status {
	case webresearch.StatusFailed:
		status = ResultStatusError
		isError = true
	case webresearch.StatusPending, webresearch.StatusRunning:
		status = ResultStatusNeutral
	}
	return Result{
		Content:  webresearch.FormatResult(result),
		Metadata: result,
		Status:   status,
		IsError:  isError,
	}, nil
}
