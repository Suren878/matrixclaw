package externalagents

import "context"

type AttachmentStore interface {
	SaveExternalAgentSession(ctx context.Context, attachment SessionAttachment) error
	GetExternalAgentSession(ctx context.Context, sessionID string) (SessionAttachment, error)
	// ListExternalAgentSessions returns the attachments of those sessions
	// that have one.
	ListExternalAgentSessions(ctx context.Context, sessionIDs []string) ([]SessionAttachment, error)
	DeleteExternalAgentSession(ctx context.Context, sessionID string) error
}
