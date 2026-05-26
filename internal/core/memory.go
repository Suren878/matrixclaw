package core

import (
	"context"
	"fmt"
	"strings"
)

const defaultMemoryLimit = 20

func (c *Core) SessionSearch(ctx context.Context, filter SearchFilter) (SessionSearchReport, error) {
	report, err := c.Search(ctx, filter)
	if err != nil {
		return SessionSearchReport{}, err
	}
	grouped := []SessionSearchResult{}
	index := map[string]int{}
	for _, match := range report.Results {
		sessionID := strings.TrimSpace(match.SessionID)
		if sessionID == "" {
			continue
		}
		pos, ok := index[sessionID]
		if !ok {
			session, err := c.store.GetSession(ctx, sessionID)
			if err != nil {
				return SessionSearchReport{}, err
			}
			grouped = append(grouped, SessionSearchResult{Session: c.decorateSessionLLM(session)})
			pos = len(grouped) - 1
			index[sessionID] = pos
		}
		grouped[pos].Matches = append(grouped[pos].Matches, match)
	}
	return SessionSearchReport{Query: report.Query, Sessions: grouped}, nil
}

func (c *Core) CreateMemory(ctx context.Context, entry MemoryEntry) (MemoryEntry, error) {
	entry.Scope = normalizeMemoryScope(entry.Scope)
	entry.Key = normalizeText(entry.Key)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.WorkingDir = normalizeMemoryWorkingDir(entry.Scope, entry.WorkingDir)
	if entry.Content == "" {
		return MemoryEntry{}, fmt.Errorf("%w: memory content is required", ErrInvalidInput)
	}
	now := c.now().UTC()
	if entry.ID == "" {
		entry.ID = c.newID("memory")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = now
	}
	if err := c.store.CreateMemory(ctx, entry); err != nil {
		return MemoryEntry{}, err
	}
	return entry, nil
}

func (c *Core) UpdateMemory(ctx context.Context, entry MemoryEntry) (MemoryEntry, error) {
	id := normalizeText(entry.ID)
	if id == "" {
		return MemoryEntry{}, fmt.Errorf("%w: memory id is required", ErrInvalidInput)
	}
	existing, err := c.store.GetMemory(ctx, id)
	if err != nil {
		return MemoryEntry{}, err
	}
	if entry.Scope != "" {
		existing.Scope = normalizeMemoryScope(entry.Scope)
	}
	if strings.TrimSpace(entry.Key) != "" {
		existing.Key = normalizeText(entry.Key)
	}
	if strings.TrimSpace(entry.Content) != "" {
		existing.Content = strings.TrimSpace(entry.Content)
	}
	if strings.TrimSpace(entry.WorkingDir) != "" || existing.Scope == MemoryScopeProject {
		existing.WorkingDir = normalizeMemoryWorkingDir(existing.Scope, entry.WorkingDir)
	}
	existing.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateMemory(ctx, existing); err != nil {
		return MemoryEntry{}, err
	}
	return existing, nil
}

func (c *Core) DeleteMemory(ctx context.Context, id string) error {
	id = normalizeText(id)
	if id == "" {
		return fmt.Errorf("%w: memory id is required", ErrInvalidInput)
	}
	return c.store.DeleteMemory(ctx, id)
}

func (c *Core) ListMemories(ctx context.Context, filter MemoryFilter) ([]MemoryEntry, error) {
	if strings.TrimSpace(string(filter.Scope)) != "" {
		filter.Scope = normalizeMemoryScope(filter.Scope)
	}
	filter.WorkingDir = normalizeWorkingDir(filter.WorkingDir)
	if filter.Limit <= 0 {
		filter.Limit = defaultMemoryLimit
	}
	return c.store.ListMemories(ctx, filter)
}

func (c *Core) MemoryPromptContext(ctx context.Context, workingDir string) string {
	if c == nil || c.store == nil {
		return ""
	}
	entries, err := c.store.ListMemories(ctx, MemoryFilter{
		WorkingDir: normalizeWorkingDir(workingDir),
		Limit:      defaultMemoryLimit,
	})
	if err != nil || len(entries) == 0 {
		return ""
	}
	lines := []string{"Memory:"}
	for _, entry := range entries {
		scope := strings.TrimSpace(string(entry.Scope))
		if scope == "" {
			scope = "global"
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
		return ""
	}
	return strings.Join(lines, "\n")
}

func normalizeMemoryScope(scope MemoryScope) MemoryScope {
	switch MemoryScope(strings.ToLower(strings.TrimSpace(string(scope)))) {
	case MemoryScopeUser:
		return MemoryScopeUser
	case MemoryScopeProject:
		return MemoryScopeProject
	case MemoryScopeGlobal, "":
		return MemoryScopeGlobal
	default:
		return MemoryScopeGlobal
	}
}

func normalizeMemoryWorkingDir(scope MemoryScope, workingDir string) string {
	if normalizeMemoryScope(scope) != MemoryScopeProject {
		return ""
	}
	return normalizeWorkingDir(workingDir)
}
