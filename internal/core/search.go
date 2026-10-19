package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const defaultSearchLimit = 20

const sessionSearchToolName = "session_search"

func (c *Core) Search(ctx context.Context, filter SearchFilter) (SearchReport, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	if filter.Query == "" {
		return SearchReport{}, fmt.Errorf("%w: search query is required", ErrInvalidInput)
	}
	if filter.Limit <= 0 {
		filter.Limit = defaultSearchLimit
	}
	results, err := c.store.SearchMessages(ctx, filter)
	if err != nil {
		return SearchReport{}, err
	}
	return SearchReport{Query: filter.Query, Results: results}, nil
}

func (c *Core) SessionSearch(ctx context.Context, filter SearchFilter) (SessionSearchReport, error) {
	report, err := c.Search(ctx, filter)
	if err != nil {
		return SessionSearchReport{}, err
	}
	grouped := []SessionSearchResult{}
	index := map[string]int{}
	for _, match := range report.Results {
		sessionID := strings.TrimSpace(match.SessionID)
		if sessionID == "" {
			continue
		}
		pos, ok := index[sessionID]
		if !ok {
			session, err := c.store.GetSession(ctx, sessionID)
			if err != nil {
				return SessionSearchReport{}, err
			}
			grouped = append(grouped, SessionSearchResult{Session: c.decorateSessionLLM(session)})
			pos = len(grouped) - 1
			index[sessionID] = pos
		}
		grouped[pos].Matches = append(grouped[pos].Matches, match)
	}
	return SessionSearchReport{Query: report.Query, Sessions: grouped}, nil
}

// NewSessionSearchTool lets models recall earlier sessions with the user.
func NewSessionSearchTool(app *Core) tools.Executor {
	return &sessionSearchTool{app: app}
}

type sessionSearchTool struct {
	app *Core
}

func (t *sessionSearchTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              sessionSearchToolName,
		Description:     "Search the messages of earlier sessions with the user; matches come back grouped by session. Use it when the user refers to an earlier conversation (\"like last time\", \"the bug we discussed\") or when past decisions matter to the task.",
		Effect:          tools.EffectReadOnly,
		Namespace:       "core.memory",
		Category:        tools.CategoryAutomation,
		InputJSONSchema: sessionSearchToolSchema,
	}
}

func (t *sessionSearchTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		Query     string `json:"query"`
		SessionID string `json:"session_id"`
		Limit     int    `json:"limit"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &input); err != nil {
			return tools.Result{}, tools.InvalidArgs(sessionSearchToolName, err)
		}
	}
	report, err := t.app.SessionSearch(ctx, SearchFilter{
		Query:     input.Query,
		SessionID: input.SessionID,
		Limit:     input.Limit,
	})
	if err != nil {
		return tools.Result{Content: err.Error(), Status: tools.ResultStatusError}, nil
	}
	return tools.Result{
		Content:  formatSessionSearchReport(report),
		Metadata: report,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func formatSessionSearchReport(report SessionSearchReport) string {
	if len(report.Sessions) == 0 {
		return "No previous session messages matched " + fmt.Sprintf("%q.", report.Query)
	}
	lines := []string{fmt.Sprintf("Found matches for %q:", report.Query)}
	for _, sessionResult := range report.Sessions {
		title := strings.TrimSpace(sessionResult.Session.Title)
		if title == "" {
			title = sessionResult.Session.ID
		}
		lines = append(lines, fmt.Sprintf("- %s (%s)", title, sessionResult.Session.ID))
		for _, match := range sessionResult.Matches {
			snippet := textutil.Clip(strings.Join(strings.Fields(match.Snippet), " "), 240, "...")
			if snippet == "" {
				snippet = match.MessageID
			}
			lines = append(lines, fmt.Sprintf("  - %s: %s", match.Role, snippet))
		}
	}
	return strings.Join(lines, "\n")
}

var sessionSearchToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Words to search for in earlier messages."},
    "session_id": {"type": "string", "description": "Search only this session."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Maximum matches to return."}
  },
  "required": ["query"],
  "additionalProperties": false
}`)
