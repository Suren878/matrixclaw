// Package todo is the agent's todo list: its items, their validation and how
// they are shown to the model.
package todo

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ToolName is the tool that replaces a session's todo list.
const ToolName = "todo_write"

// MaxItems bounds a list so it stays a plan of the work, not a log of it.
const MaxItems = 50

// Status is where an item stands.
type Status string

const (
	Pending    Status = "pending"
	InProgress Status = "in_progress"
	Completed  Status = "completed"
)

// Item is one piece of work; ActiveForm names it while it is in progress.
type Item struct {
	Content    string `json:"content"`
	ActiveForm string `json:"active_form,omitempty"`
	Status     Status `json:"status"`
}

// Label is how the item is shown: its active form while it is in progress.
func (i Item) Label() string {
	if i.Status == InProgress && i.ActiveForm != "" {
		return i.ActiveForm
	}
	return i.Content
}

// List is a session's todo list. ChainRunID is the first run of the run chain
// that wrote it, UpdatedRunID the run that wrote it last.
type List struct {
	SessionID    string    `json:"session_id"`
	Items        []Item    `json:"items"`
	ChainRunID   string    `json:"chain_run_id,omitempty"`
	UpdatedRunID string    `json:"updated_run_id,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitzero"`
}

// InChain reports whether chain (a run and the runs it continues) wrote the
// list or began the chain it was written in.
func (l List) InChain(chain []string) bool {
	for _, id := range []string{l.UpdatedRunID, l.ChainRunID} {
		if id != "" && slices.Contains(chain, id) {
			return true
		}
	}
	return false
}

// Open lists the items not completed yet.
func Open(items []Item) []Item {
	var open []Item
	for _, item := range items {
		if item.Status != Completed {
			open = append(open, item)
		}
	}
	return open
}

// Text renders items for the model, one numbered line each.
func Text(items []Item) string {
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, fmt.Sprintf("%d. [%s] %s", i+1, item.Status, item.Content))
	}
	return strings.Join(lines, "\n")
}

// Parse reads todo_write arguments into a checked list; its errors tell the
// model what to correct.
func Parse(args json.RawMessage) ([]Item, error) {
	var input struct {
		Items *[]Item `json:"items"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("arguments are not a JSON object with an items array: %w", err)
	}
	if input.Items == nil {
		return nil, errors.New("items is required; send an empty array to clear the list")
	}
	items := *input.Items
	if len(items) > MaxItems {
		return nil, fmt.Errorf("the list has %d items; keep it to %d", len(items), MaxItems)
	}
	out := make([]Item, 0, len(items))
	inProgress := 0
	for i, item := range items {
		item.Content = strings.TrimSpace(item.Content)
		item.ActiveForm = strings.TrimSpace(item.ActiveForm)
		item.Status = Status(strings.ToLower(strings.TrimSpace(string(item.Status))))
		if item.Content == "" {
			return nil, fmt.Errorf("item %d has no content", i+1)
		}
		switch item.Status {
		case Pending, Completed:
		case InProgress:
			inProgress++
		default:
			return nil, fmt.Errorf("item %d has status %q; use pending, in_progress or completed", i+1, item.Status)
		}
		out = append(out, item)
	}
	if inProgress > 1 {
		return nil, fmt.Errorf("%d items are in_progress; only one may be in progress at a time", inProgress)
	}
	return out, nil
}
