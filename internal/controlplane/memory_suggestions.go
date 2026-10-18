package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

// The memory entries session reviews suggested, which runs see once the
// owner approves them.
var (
	memoryPending        = verb(CommandMemory, "pending", 0)
	memoryPendingShow    = verb(CommandMemory, "pending show", 1)
	memoryPendingApprove = verb(CommandMemory, "pending approve", 1)
	memoryPendingReject  = verb(CommandMemory, "pending reject", 1)
)

// handleMemorySuggestions lists the suggestions, shows one, and approves or
// rejects it.
func (d *Dispatcher) handleMemorySuggestions(ctx context.Context, r Route) (Result, error) {
	if !d.owner() {
		return textResult("Only the owner can review memory suggestions."), nil
	}
	var err error
	switch {
	case r.Is(memoryPendingShow):
		return d.memorySuggestionPicker(ctx, r.arg(0))
	case r.Is(memoryPendingApprove):
		err = d.daemon.ApproveMemory(ctx, r.arg(0))
	case r.Is(memoryPendingReject):
		err = d.daemon.DeleteMemory(ctx, r.arg(0))
	}
	if err != nil {
		return textResult(err.Error()), nil
	}
	return d.memorySuggestionsPicker(ctx)
}

func (d *Dispatcher) memorySuggestions(ctx context.Context) ([]core.MemoryEntry, error) {
	return d.daemon.ListMemories(ctx, core.MemoryFilter{Pending: true, Limit: 50})
}

func (d *Dispatcher) memorySuggestionsPicker(ctx context.Context) (Result, error) {
	entries, err := d.memorySuggestions(ctx)
	if err != nil {
		return Result{}, err
	}
	picker := NewPickerData(PickerMemoryPending, "Memory Suggestions").
		Meta("Session reviews suggested these; runs see an entry once you approve it.").
		Command(memoryPending.command())
	for _, entry := range entries {
		picker.Item(PickerItem{ID: entry.ID, Title: entry.Content, Info: memorySuggestionScope(entry), Command: memoryPendingShow.command(entry.ID)})
	}
	if len(entries) == 0 {
		picker.Static("empty", "No suggestions", "Session reviews leave memory suggestions here.")
	}
	return Result{Screen: picker.Build()}, nil
}

func (d *Dispatcher) memorySuggestionPicker(ctx context.Context, id string) (Result, error) {
	entries, err := d.memorySuggestions(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, entry := range entries {
		if entry.ID != id {
			continue
		}
		picker := NewPickerData(PickerMemoryPending, "Memory Suggestion").
			Meta(memorySuggestionScope(entry)+": "+entry.Content).
			Back(memoryPending.command()).
			Action("approve", "Approve", "Remember it in every later run", memoryPendingApprove.command(entry.ID)).
			Danger("reject", "Reject", "", memoryPendingReject.command(entry.ID))
		return Result{Screen: picker.Build()}, nil
	}
	return d.memorySuggestionsPicker(ctx)
}

func memorySuggestionScope(entry core.MemoryEntry) string {
	if entry.Scope == core.MemoryScopeProject && strings.TrimSpace(entry.WorkingDir) != "" {
		return "project " + entry.WorkingDir
	}
	return string(entry.Scope)
}
