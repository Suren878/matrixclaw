package skills

import (
	"slices"
	"strings"
	"time"
)

const (
	TrustTrusted    = "trusted"
	TrustQuarantine = "quarantine"

	StateActive   = "active"
	StateArchived = "archived"
)

// Action changes a skill's trust, state or place in the library.
type Action string

const (
	ActionTrust      Action = "trust"
	ActionQuarantine Action = "quarantine"
	ActionEnable     Action = "enable"
	ActionDisable    Action = "disable"
	ActionArchive    Action = "archive"
	ActionRestore    Action = "restore"
	ActionPin        Action = "pin"
	ActionUnpin      Action = "unpin"
	ActionRemove     Action = "remove"
)

// Actions lists every Action.
var Actions = []Action{ActionTrust, ActionQuarantine, ActionEnable, ActionDisable, ActionArchive, ActionRestore, ActionPin, ActionUnpin, ActionRemove}

// ParseAction reads an Action by name, ignoring case and spaces.
func ParseAction(name string) (Action, bool) {
	action := Action(strings.ToLower(strings.TrimSpace(name)))
	return action, slices.Contains(Actions, action)
}

// Settings are the library's settings from setup.json.
type Settings struct {
	AutoInvoke bool
	// TrustPolicy "trusted" trusts installed skills; anything else
	// quarantines them.
	TrustPolicy string
	// GitEnv is the environment of git clone: no daemon keys.
	GitEnv []string
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

// SkillsResponse is a list of skills on the daemon API.
type SkillsResponse struct {
	Skills []Skill `json:"skills"`
}

// InstallRequest installs the skills found at a local path.
type InstallRequest struct {
	Path string `json:"path"`
}

// DraftRequest creates a draft skill.
type DraftRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Body        string   `json:"body,omitempty"`
}

// BodyRequest replaces a skill's instructions.
type BodyRequest struct {
	Body string `json:"body"`
}
