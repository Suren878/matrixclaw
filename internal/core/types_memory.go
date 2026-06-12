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
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type MemoryFilter struct {
	Scope      MemoryScope
	WorkingDir string
	Limit      int
}
