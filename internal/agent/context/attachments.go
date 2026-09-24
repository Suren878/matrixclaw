// Package agentcontext builds the model's view of a session: provider conversation,
// compaction markers and summaries, and token estimates.
package agentcontext

import (
	"context"
	"errors"
)

type AttachmentData struct {
	Data     []byte
	MIMEType string
	Name     string
	Size     int64
}

// ErrAttachmentUnavailable identifies an attachment that used to exist but can
// no longer be read, for example because a temporary upload expired. Provider
// conversation building can omit the binary data without failing the whole run.
var ErrAttachmentUnavailable = errors.New("attachment is no longer available")

type AttachmentReader interface {
	ReadAttachment(ctx context.Context, path string, temporary bool, maxBytes int64) (AttachmentData, error)
}
