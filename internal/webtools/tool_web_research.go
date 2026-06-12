package webtools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Suren878/matrixclaw/internal/tools"

	"github.com/Suren878/matrixclaw/internal/webresearch"
)

type webResearchExecutor struct {
	name string
	web  *WebService
}

func NewWebResearchExecutors(engine *webresearch.Engine) []tools.Executor {
	return NewWebResearchExecutorsWithService(NewWebService(nil, engine))
}

func NewWebResearchExecutorsWithService(web *WebService) []tools.Executor {
	return []tools.Executor{
		&webResearchExecutor{name: webResearchToolName, web: web},
		&webResearchExecutor{name: webResearchAskToolName, web: web},
		&webResearchExecutor{name: webResearchStatusToolName, web: web},
	}
}

func (e *webResearchExecutor) Spec() tools.Spec {
	switch e.name {
	case webResearchAskToolName:
		return tools.Spec{
			ID:              webResearchAskToolName,
			Name:            "WebResearchAsk",
			Description:     "Ask a follow-up question against a previous web_research session; reuses stored facts/artifacts before refetching",
			Risk:            tools.RiskSafe,
			Effect:          tools.EffectReadOnly,
			ApprovalMode:    tools.ApprovalNever,
			Namespace:       namespaceCoreWeb,
			Category:        tools.CategoryWeb,
			Profiles:        []tools.Profile{tools.ProfileCoding, tools.ProfileWeb},
			OutputKind:      tools.OutputSearchResults,
			InputJSONSchema: webResearchAskInputSchema,
		}
	case webResearchStatusToolName:
		return tools.Spec{
			ID:              webResearchStatusToolName,
			Name:            "WebResearchStatus",
			Description:     "Check status and compact results for a web_research session by research_id",
			Risk:            tools.RiskSafe,
			Effect:          tools.EffectReadOnly,
			ApprovalMode:    tools.ApprovalNever,
			Namespace:       namespaceCoreWeb,
			Category:        tools.CategoryWeb,
			Profiles:        []tools.Profile{tools.ProfileCoding, tools.ProfileWeb},
			OutputKind:      tools.OutputJob,
			InputJSONSchema: webResearchStatusInputSchema,
		}
	default:
		return tools.Spec{
			ID:              webResearchToolName,
			Name:            "WebResearch",
			Description:     "Research the web using search/fetch/browser fallback and return only compact facts, sources, warnings, next actions, and a research_id",
			Risk:            tools.RiskSafe,
			Effect:          tools.EffectReadOnly,
			ApprovalMode:    tools.ApprovalNever,
			Namespace:       namespaceCoreWeb,
			Category:        tools.CategoryWeb,
			Profiles:        []tools.Profile{tools.ProfileCoding, tools.ProfileWeb},
			OutputKind:      tools.OutputSearchResults,
			InputJSONSchema: webResearchInputSchema,
		}
	}
}

func (e *webResearchExecutor) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if e == nil || e.web == nil || !e.web.ResearchConfigured() {
		return tools.Result{Content: "web research engine is not configured", Status: tools.ResultStatusError, IsError: true}, nil
	}
	var result webresearch.ResearchResult
	var err error
	switch e.name {
	case webResearchAskToolName:
		var input webresearch.AskRequest
		if decodeErr := json.Unmarshal(call.Args, &input); decodeErr != nil {
			return tools.Result{}, tools.InvalidArgs(webResearchAskToolName, decodeErr)
		}
		result, err = e.web.Ask(ctx, input)
	case webResearchStatusToolName:
		var input webresearch.StatusRequest
		if decodeErr := json.Unmarshal(call.Args, &input); decodeErr != nil {
			return tools.Result{}, tools.InvalidArgs(webResearchStatusToolName, decodeErr)
		}
		result, err = e.web.Status(ctx, input)
	default:
		var input webresearch.ResearchRequest
		if decodeErr := json.Unmarshal(call.Args, &input); decodeErr != nil {
			return tools.Result{}, tools.InvalidArgs(webResearchToolName, decodeErr)
		}
		result, err = e.web.Research(ctx, input)
	}
	if err != nil {
		return tools.Result{
			Content: fmt.Sprintf("%s failed: %v", e.name, err),
			Status:  tools.ResultStatusError,
			IsError: true,
		}, nil
	}
	status := tools.ResultStatusSuccess
	isError := false
	switch result.Status {
	case webresearch.StatusFailed:
		status = tools.ResultStatusError
		isError = true
	case webresearch.StatusPending, webresearch.StatusRunning:
		status = tools.ResultStatusNeutral
	}
	return tools.Result{
		Content:  webresearch.FormatResult(result),
		Metadata: result,
		Status:   status,
		IsError:  isError,
	}, nil
}
