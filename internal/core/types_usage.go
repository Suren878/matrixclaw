package core

import "time"

// UsageRecord sums the run_steps of one run; Provider and Model are the last step's.
type UsageRecord struct {
	SessionID        string    `json:"session_id"`
	RunID            string    `json:"run_id"`
	Provider         string    `json:"provider,omitempty"`
	Model            string    `json:"model,omitempty"`
	Steps            int       `json:"steps"`
	PromptTokens     int64     `json:"prompt_tokens,omitempty"`
	CacheReadTokens  int64     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64     `json:"cache_write_tokens,omitempty"`
	OutputTokens     int64     `json:"output_tokens,omitempty"`
	ReasoningTokens  int64     `json:"reasoning_tokens,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type UsageFilter struct {
	SessionID string
	RunID     string
	Limit     int
}

type UsageSummary struct {
	SessionID        string `json:"session_id,omitempty"`
	Runs             int    `json:"runs"`
	Steps            int    `json:"steps"`
	PromptTokens     int64  `json:"prompt_tokens,omitempty"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	OutputTokens     int64  `json:"output_tokens,omitempty"`
	ReasoningTokens  int64  `json:"reasoning_tokens,omitempty"`
}

type UsageReport struct {
	Summary UsageSummary  `json:"summary"`
	Records []UsageRecord `json:"records,omitempty"`
}
