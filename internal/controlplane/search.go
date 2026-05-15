package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleSearch(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.search == nil {
		return unsupportedRuntime("search"), nil
	}
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	query := strings.TrimSpace(args)
	if query == "" {
		return Result{Handled: true, Text: "Usage: /search <query>"}, nil
	}
	_, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	sessionID := ""
	if session != nil {
		sessionID = session.ID
	}
	report, err := d.search.Search(ctx, core.SearchFilter{Query: query, SessionID: sessionID, Limit: 20})
	if err != nil {
		return Result{}, err
	}
	info := searchInfoData(report)
	return Result{Handled: true, Info: &info}, nil
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
