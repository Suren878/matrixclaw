package controlplane

import (
	"context"
	"fmt"
	"github.com/Suren878/matrixclaw/internal/memory"
	"strings"
)

// A scope to list is the value of /memory.
var _ = valueVerb(CommandMemory, "", 0)

func (d *Dispatcher) handleMemory(ctx context.Context, r Route) (Result, error) {
	if r.Verb.words != "" {
		return d.handleMemorySuggestions(ctx, r)
	}
	filter := memory.Filter{
		Scope:      memoryScopeArg(r.Value),
		WorkingDir: d.workingDir,
		Limit:      50,
	}
	entries, err := d.daemon.ListMemories(ctx, filter)
	if err != nil {
		return Result{}, err
	}
	return Result{Screen: memoryInfoData(entries)}, nil
}

func memoryScopeArg(args string) memory.Scope {
	switch memory.Scope(strings.ToLower(strings.TrimSpace(args))) {
	case memory.ScopeGlobal:
		return memory.ScopeGlobal
	case memory.ScopeUser:
		return memory.ScopeUser
	case memory.ScopeProject:
		return memory.ScopeProject
	default:
		return ""
	}
}

func memoryInfoData(entries []memory.Entry) InfoData {
	rows := make([]InfoRow, 0, len(entries)+1)
	rows = append(rows, InfoRow{Label: "Entries", Value: fmt.Sprintf("%d", len(entries))})
	for _, entry := range entries {
		label := string(entry.Scope)
		if strings.TrimSpace(entry.Key) != "" {
			label += "/" + strings.TrimSpace(entry.Key)
		}
		if label == "" {
			label = string(memory.ScopeGlobal)
		}
		rows = append(rows, InfoRow{Label: label, Value: strings.TrimSpace(entry.Content)})
	}
	return InfoData{
		Title: "Memory",
		Text:  memoryInfoText(entries),
		Rows:  rows,
	}
}

func memoryInfoText(entries []memory.Entry) string {
	if len(entries) == 0 {
		return "No memory entries."
	}
	lines := []string{"Memory:"}
	for _, entry := range entries {
		label := strings.TrimSpace(string(entry.Scope))
		if label == "" {
			label = string(memory.ScopeGlobal)
		}
		if strings.TrimSpace(entry.Key) != "" {
			label += "/" + strings.TrimSpace(entry.Key)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", label, strings.TrimSpace(entry.Content)))
	}
	return strings.Join(lines, "\n")
}
