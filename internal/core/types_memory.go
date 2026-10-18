package core

import "time"

type MemoryScope string

const (
	MemoryScopeGlobal  MemoryScope = "global"
	MemoryScopeUser    MemoryScope = "user"
	MemoryScopeProject MemoryScope = "project"
)

type MemoryEntry struct {
	ID         string      `json:"id"`
	Scope      MemoryScope `json:"scope"`
	Key        string      `json:"key,omitempty"`
	Content    string      `json:"content"`
	WorkingDir string      `json:"working_dir,omitempty"`
	// Pending marks a suggestion of a session review that waits for the
	// owner's approval; runs see only approved entries.
	Pending   bool      `json:"pending,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MemoryFilter selects approved entries, or with Pending the suggestions.
type MemoryFilter struct {
	Scope      MemoryScope
	WorkingDir string
	Pending    bool
	Limit      int
}
