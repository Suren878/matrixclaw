package skills

import "time"

const (
	TrustTrusted    = "trusted"
	TrustQuarantine = "quarantine"
	TrustDisabled   = "disabled"

	StateActive   = "active"
	StateArchived = "archived"

	DefaultTrustPolicy = "quarantine"
	DefaultSelfImprove = "drafts"
)

type Config struct {
	DBPath      string
	Root        string
	Enabled     bool
	AutoInvoke  bool
	TrustPolicy string
	SelfImprove string
}

type Document struct {
	Name        string
	Description string
	Version     string
	Author      string
	Authors     []string
	License     string
	Tags        []string
	Platforms   []string
	Category    string
	Body        string
	Metadata    map[string]any
}

type Skill struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Version        string    `json:"version,omitempty"`
	Author         string    `json:"author,omitempty"`
	Authors        []string  `json:"authors,omitempty"`
	License        string    `json:"license,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	Platforms      []string  `json:"platforms,omitempty"`
	Category       string    `json:"category,omitempty"`
	Path           string    `json:"path,omitempty"`
	Source         string    `json:"source,omitempty"`
	Provenance     string    `json:"provenance,omitempty"`
	Hash           string    `json:"hash,omitempty"`
	TrustState     string    `json:"trust_state"`
	State          string    `json:"state"`
	Enabled        bool      `json:"enabled"`
	Pinned         bool      `json:"pinned,omitempty"`
	UseCount       int64     `json:"use_count,omitempty"`
	ViewCount      int64     `json:"view_count,omitempty"`
	PatchCount     int64     `json:"patch_count,omitempty"`
	InstalledAt    time.Time `json:"installed_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
}

type SkillDetail struct {
	Skill Skill  `json:"skill"`
	Body  string `json:"body"`
}

type MetadataUpdate struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Category    string   `json:"category,omitempty"`
}

type InstallOptions struct {
	Provenance string
	Source     string
	TrustState string
}

type SearchOptions struct {
	IncludeQuarantined bool
	IncludeArchived    bool
	IncludeDisabled    bool
	Limit              int
}

type PromptMessage struct {
	Role    string
	Content string
}

type PromptRequest struct {
	SessionID  string
	RunID      string
	WorkingDir string
	Messages   []PromptMessage
}

type UsageSummary struct {
	Skills []Skill `json:"skills"`
}

type CuratorResult struct {
	Archived []Skill `json:"archived"`
}
