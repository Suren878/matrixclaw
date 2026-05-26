package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleMemory(ctx context.Context, args string) (Result, error) {
	if d.memory == nil {
		return unsupportedRuntime("memory"), nil
	}
	filter := core.MemoryFilter{
		Scope:      memoryScopeArg(args),
		WorkingDir: d.workingDir,
		Limit:      50,
	}
	entries, err := d.memory.ListMemories(ctx, filter)
	if err != nil {
		return Result{}, err
	}
	info := memoryInfoData(entries)
	return Result{Handled: true, Info: &info}, nil
}

func memoryScopeArg(args string) core.MemoryScope {
	switch core.MemoryScope(strings.ToLower(strings.TrimSpace(args))) {
	case core.MemoryScopeGlobal:
		return core.MemoryScopeGlobal
	case core.MemoryScopeUser:
		return core.MemoryScopeUser
	case core.MemoryScopeProject:
		return core.MemoryScopeProject
	default:
		return ""
	}
}

func memoryInfoData(entries []core.MemoryEntry) InfoData {
	rows := make([]InfoRow, 0, len(entries)+1)
	rows = append(rows, InfoRow{Label: "Entries", Value: fmt.Sprintf("%d", len(entries))})
	for _, entry := range entries {
		label := string(entry.Scope)
		if strings.TrimSpace(entry.Key) != "" {
			label += "/" + strings.TrimSpace(entry.Key)
		}
		if label == "" {
			label = string(core.MemoryScopeGlobal)
		}
		rows = append(rows, InfoRow{Label: label, Value: strings.TrimSpace(entry.Content)})
	}
	return InfoData{
		Title: "Memory",
		Text:  memoryInfoText(entries),
		Rows:  rows,
	}
}

func memoryInfoText(entries []core.MemoryEntry) string {
	if len(entries) == 0 {
		return "No memory entries."
	}
	lines := []string{"Memory:"}
	for _, entry := range entries {
		label := strings.TrimSpace(string(entry.Scope))
		if label == "" {
			label = string(core.MemoryScopeGlobal)
		}
		if strings.TrimSpace(entry.Key) != "" {
			label += "/" + strings.TrimSpace(entry.Key)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", label, strings.TrimSpace(entry.Content)))
	}
	return strings.Join(lines, "\n")
}
