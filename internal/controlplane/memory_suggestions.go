package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

// memoryPendingCommand is the screen of the memory entries session reviews
// suggested, which runs see once the owner approves them.
func memoryPendingCommand(parts ...string) string {
	return controlplaneCommand(append([]string{"memory", "pending"}, parts...)...)
}

// handleMemorySuggestions serves /memory pending [<id> [approve|reject]].
func (d *Dispatcher) handleMemorySuggestions(ctx context.Context, args string) (Result, error) {
	if !d.owner() {
		return Result{Handled: true, Text: "Only the owner can review memory suggestions."}, nil
	}
	id, rest := firstCommandToken(args)
	action, _ := firstCommandStep(rest)
	var err error
	switch {
	case id == "":
		return d.memorySuggestionsPicker(ctx)
	case action == "approve":
		err = d.daemon.ApproveMemory(ctx, id)
	case action == "reject":
		err = d.daemon.DeleteMemory(ctx, id)
	default:
		return d.memorySuggestionPicker(ctx, id)
	}
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
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
		Command(memoryPendingCommand())
	for _, entry := range entries {
		picker.Item(PickerItem{ID: entry.ID, Title: entry.Content, Info: memorySuggestionScope(entry), Command: memoryPendingCommand(entry.ID)})
	}
	if len(entries) == 0 {
		picker.Static("empty", "No suggestions", "Session reviews leave memory suggestions here.")
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
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
			Back(memoryPendingCommand()).
			Action("approve", "Approve", "Remember it in every later run", memoryPendingCommand(entry.ID, "approve")).
			Danger("reject", "Reject", "", memoryPendingCommand(entry.ID, "reject"))
		return Result{Handled: true, Picker: picker.Ptr()}, nil
	}
	return d.memorySuggestionsPicker(ctx)
}

func memorySuggestionScope(entry core.MemoryEntry) string {
	if entry.Scope == core.MemoryScopeProject && strings.TrimSpace(entry.WorkingDir) != "" {
		return "project " + entry.WorkingDir
	}
	return string(entry.Scope)
}
