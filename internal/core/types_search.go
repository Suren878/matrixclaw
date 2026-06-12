package core

import "time"

type SearchFilter struct {
	Query     string
	SessionID string
	Limit     int
}

type SearchResult struct {
	MessageID string    `json:"message_id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role,omitempty"`
	Snippet   string    `json:"snippet,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	Rank      float64   `json:"rank,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type SearchReport struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

type SessionSearchResult struct {
	Session Session        `json:"session"`
	Matches []SearchResult `json:"matches"`
}

type SessionSearchReport struct {
	Query    string                `json:"query"`
	Sessions []SessionSearchResult `json:"sessions"`
}
