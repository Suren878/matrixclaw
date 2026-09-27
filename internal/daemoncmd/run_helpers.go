package daemoncmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type storageAttachmentReader struct {
	store *localstorage.LocalStore
}

func (r storageAttachmentReader) ReadAttachment(ctx context.Context, storagePath string, temporary bool, maxBytes int64) (agentcontext.AttachmentData, error) {
	if r.store == nil {
		return agentcontext.AttachmentData{}, localstorage.ErrInvalidPath
	}
	if temporary {
		entry, data, err := r.store.ReadTemporaryBytes(storagePath)
		if err != nil {
			return agentcontext.AttachmentData{}, normalizeAttachmentReadError(err)
		}
		return agentcontext.AttachmentData{
			Data:     data,
			MIMEType: entry.MIMEType,
			Name:     entry.Title,
			Size:     entry.Size,
		}, nil
	}
	entry, data, err := r.store.ReadBytes(storagePath, maxBytes)
	if err != nil {
		return agentcontext.AttachmentData{}, normalizeAttachmentReadError(err)
	}
	return agentcontext.AttachmentData{
		Data:     data,
		MIMEType: entry.MIMEType,
		Name:     entry.Title,
		Size:     entry.Size,
	}, nil
}

func normalizeAttachmentReadError(err error) error {
	if errors.Is(err, localstorage.ErrNotFound) {
		return fmt.Errorf("%w: %w", agentcontext.ErrAttachmentUnavailable, err)
	}
	return err
}

// dataDir is the directory of the database, which holds the daemon's files.
func dataDir(dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = setup.DefaultDBPath()
	}
	if abs, err := filepath.Abs(dbPath); err == nil {
		dbPath = abs
	}
	return filepath.Dir(dbPath)
}

func defaultStorageRoot(dbPath string) string {
	return filepath.Join(dataDir(dbPath), "storage")
}

// sessionFilesRoot holds per-session files such as full tool outputs.
func sessionFilesRoot(dbPath string) string {
	return filepath.Join(dataDir(dbPath), "sessions")
}

func appendModuleContext(systemPrompt string, contexts []string) string {
	if len(contexts) == 0 {
		return strings.TrimSpace(systemPrompt)
	}
	context := strings.TrimSpace(strings.Join(contexts, "\n"))
	if context == "" {
		return strings.TrimSpace(systemPrompt)
	}
	systemPrompt = strings.TrimSpace(systemPrompt)
	if systemPrompt == "" {
		return "Enabled modules:\n" + context
	}
	return systemPrompt + "\n\nEnabled modules:\n" + context
}

func daemonBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	return "http://" + addr
}
