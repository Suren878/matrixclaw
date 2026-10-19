// Package memory keeps the persistent notes native runs see at their start:
// entries the agent or the owner wrote and suggestions waiting for approval.
package memory

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/ids"
)

// DefaultLimit is how many entries a listing and the prompt hold.
const DefaultLimit = 20

// ErrInvalidInput reports an entry or filter memory refuses.
var ErrInvalidInput = errors.New("invalid memory input")

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

// ParseScope reads a scope in any case; empty is global.
func ParseScope(value string) (Scope, error) {
	switch scope := Scope(strings.ToLower(strings.TrimSpace(value))); scope {
	case "":
		return ScopeGlobal, nil
	case ScopeGlobal, ScopeUser, ScopeProject:
		return scope, nil
	default:
		return "", fmt.Errorf("%w: unknown scope %q (use global, user or project)", ErrInvalidInput, value)
	}
}

type Entry struct {
	ID         string `json:"id"`
	Scope      Scope  `json:"scope"`
	Key        string `json:"key,omitempty"`
	Content    string `json:"content"`
	WorkingDir string `json:"working_dir,omitempty"`
	// Pending marks a suggestion of a session review that waits for the
	// owner's approval; runs see only approved entries.
	Pending   bool      `json:"pending,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Filter selects approved entries, or with Pending the suggestions.
type Filter struct {
	Scope      Scope
	WorkingDir string
	Pending    bool
	Limit      int
}

type Store interface {
	CreateMemory(ctx context.Context, entry Entry) error
	UpdateMemory(ctx context.Context, entry Entry) error
	DeleteMemory(ctx context.Context, id string) error
	GetMemory(ctx context.Context, id string) (Entry, error)
	ListMemories(ctx context.Context, filter Filter) ([]Entry, error)
}

// Service checks and stamps entries before the store keeps them.
type Service struct {
	store Store
	now   func() time.Time
}

// New keeps memory in store; a nil now is time.Now.
func New(store Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}

func (s *Service) Create(ctx context.Context, entry Entry) (Entry, error) {
	scope, err := ParseScope(string(entry.Scope))
	if err != nil {
		return Entry{}, err
	}
	entry.Scope = scope
	entry.Key = strings.TrimSpace(entry.Key)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.WorkingDir = scopedWorkingDir(entry.Scope, entry.WorkingDir)
	if entry.Content == "" {
		return Entry{}, fmt.Errorf("%w: memory content is required", ErrInvalidInput)
	}
	now := s.now().UTC()
	if entry.ID == "" {
		entry.ID = ids.New("memory")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = now
	}
	if err := s.store.CreateMemory(ctx, entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// Update replaces the fields entry sets of the entry with its ID.
func (s *Service) Update(ctx context.Context, entry Entry) (Entry, error) {
	id := strings.TrimSpace(entry.ID)
	if id == "" {
		return Entry{}, fmt.Errorf("%w: memory id is required", ErrInvalidInput)
	}
	var scope Scope
	if strings.TrimSpace(string(entry.Scope)) != "" {
		var err error
		if scope, err = ParseScope(string(entry.Scope)); err != nil {
			return Entry{}, err
		}
	}
	existing, err := s.store.GetMemory(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	if scope != "" {
		existing.Scope = scope
	}
	if strings.TrimSpace(entry.Key) != "" {
		existing.Key = strings.TrimSpace(entry.Key)
	}
	if strings.TrimSpace(entry.Content) != "" {
		existing.Content = strings.TrimSpace(entry.Content)
	}
	if strings.TrimSpace(entry.WorkingDir) != "" || existing.Scope == ScopeProject {
		existing.WorkingDir = scopedWorkingDir(existing.Scope, entry.WorkingDir)
	}
	existing.UpdatedAt = s.now().UTC()
	if err := s.store.UpdateMemory(ctx, existing); err != nil {
		return Entry{}, err
	}
	return existing, nil
}

// Approve turns a suggested entry into one that runs see.
func (s *Service) Approve(ctx context.Context, id string) (Entry, error) {
	entry, err := s.store.GetMemory(ctx, strings.TrimSpace(id))
	if err != nil {
		return Entry{}, err
	}
	if !entry.Pending {
		return entry, nil
	}
	entry.Pending = false
	entry.UpdatedAt = s.now().UTC()
	if err := s.store.UpdateMemory(ctx, entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: memory id is required", ErrInvalidInput)
	}
	return s.store.DeleteMemory(ctx, id)
}

// List lists the entries filter selects, of every scope when it names none.
func (s *Service) List(ctx context.Context, filter Filter) ([]Entry, error) {
	if strings.TrimSpace(string(filter.Scope)) != "" {
		scope, err := ParseScope(string(filter.Scope))
		if err != nil {
			return nil, err
		}
		filter.Scope = scope
	}
	filter.WorkingDir = cleanDir(filter.WorkingDir)
	if filter.Limit <= 0 {
		filter.Limit = DefaultLimit
	}
	return s.store.ListMemories(ctx, filter)
}

// PromptContext is the memory section of the prompt of a run in workingDir;
// empty without entries.
func (s *Service) PromptContext(ctx context.Context, workingDir string) (string, error) {
	entries, err := s.store.ListMemories(ctx, Filter{WorkingDir: cleanDir(workingDir), Limit: DefaultLimit})
	if err != nil {
		return "", err
	}
	lines := []string{"Memory:"}
	for _, entry := range entries {
		scope := strings.TrimSpace(string(entry.Scope))
		if scope == "" {
			scope = string(ScopeGlobal)
		}
		content := strings.TrimSpace(entry.Content)
		if content == "" {
			continue
		}
		if entry.Key != "" {
			lines = append(lines, fmt.Sprintf("- %s/%s: %s", scope, entry.Key, content))
		} else {
			lines = append(lines, fmt.Sprintf("- %s: %s", scope, content))
		}
	}
	if len(lines) == 1 {
		return "", nil
	}
	return strings.Join(lines, "\n"), nil
}

// scopedWorkingDir is the directory a project entry is kept for; other
// scopes have none.
func scopedWorkingDir(scope Scope, workingDir string) string {
	if scope != ScopeProject {
		return ""
	}
	return cleanDir(workingDir)
}

func cleanDir(dir string) string {
	if dir = strings.TrimSpace(dir); dir == "" {
		return ""
	}
	return filepath.Clean(dir)
}
