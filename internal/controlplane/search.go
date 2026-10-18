package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

// The query is the value of /search.
var _ = valueVerb(CommandSearch, "", 0)

func (d *Dispatcher) handleSearch(ctx context.Context, r Route) (Result, error) {
	query := r.Value
	if query == "" {
		return textResult("Usage: /search <query>"), nil
	}
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	sessionID := ""
	if session != nil {
		sessionID = session.ID
	}
	report, err := d.daemon.Search(ctx, core.SearchFilter{Query: query, SessionID: sessionID, Limit: 20})
	if err != nil {
		return Result{}, err
	}
	return Result{Screen: searchInfoData(report)}, nil
}

func searchInfoData(report core.SearchReport) InfoData {
	rows := make([]InfoRow, 0, len(report.Results)+1)
	rows = append(rows, InfoRow{Label: "Query", Value: report.Query})
	for i, result := range report.Results {
		label := fmt.Sprintf("%d. %s", i+1, result.Role)
		value := strings.TrimSpace(result.Snippet)
		if value == "" {
			value = result.MessageID
		}
		rows = append(rows, InfoRow{Label: label, Value: value})
	}
	return InfoData{
		Title: "Search",
		Text:  searchInfoText(report),
		Rows:  rows,
	}
}

func searchInfoText(report core.SearchReport) string {
	if len(report.Results) == 0 {
		return "No results for: " + report.Query
	}
	lines := []string{"Search: " + report.Query}
	for i, result := range report.Results {
		lines = append(lines, fmt.Sprintf("%d. %s %s", i+1, result.Role, strings.TrimSpace(result.Snippet)))
	}
	return strings.Join(lines, "\n")
}
