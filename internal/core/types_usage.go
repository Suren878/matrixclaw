package core

import "time"

type UsageRecord struct {
	ID              string    `json:"id"`
	SessionID       string    `json:"session_id"`
	RunID           string    `json:"run_id"`
	MessageID       string    `json:"message_id,omitempty"`
	Provider        string    `json:"provider,omitempty"`
	Model           string    `json:"model,omitempty"`
	InputTokens     int64     `json:"input_tokens,omitempty"`
	OutputTokens    int64     `json:"output_tokens,omitempty"`
	TotalTokens     int64     `json:"total_tokens,omitempty"`
	CachedTokens    int64     `json:"cached_tokens,omitempty"`
	ReasoningTokens int64     `json:"reasoning_tokens,omitempty"`
	Estimated       bool      `json:"estimated,omitempty"`
	ProviderRaw     string    `json:"provider_raw,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type UsageFilter struct {
	SessionID string
	RunID     string
	Limit     int
}

type UsageSummary struct {
	SessionID       string `json:"session_id,omitempty"`
	Runs            int    `json:"runs"`
	InputTokens     int64  `json:"input_tokens,omitempty"`
	OutputTokens    int64  `json:"output_tokens,omitempty"`
	TotalTokens     int64  `json:"total_tokens,omitempty"`
	CachedTokens    int64  `json:"cached_tokens,omitempty"`
	ReasoningTokens int64  `json:"reasoning_tokens,omitempty"`
}

type UsageReport struct {
	Summary UsageSummary  `json:"summary"`
	Records []UsageRecord `json:"records,omitempty"`
}
