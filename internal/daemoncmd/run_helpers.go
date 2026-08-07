package daemoncmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type storageAttachmentReader struct {
	store *localstorage.LocalStore
}

func (r storageAttachmentReader) ReadAttachment(ctx context.Context, storagePath string, temporary bool, maxBytes int64) (core.AttachmentData, error) {
	if r.store == nil {
		return core.AttachmentData{}, localstorage.ErrInvalidPath
	}
	if temporary {
		entry, data, err := r.store.ReadTemporaryBytes(storagePath)
		if err != nil {
			return core.AttachmentData{}, normalizeAttachmentReadError(err)
		}
		return core.AttachmentData{
			Data:     data,
			MIMEType: entry.MIMEType,
			Name:     entry.Title,
			Size:     entry.Size,
		}, nil
	}
	entry, data, err := r.store.ReadBytes(storagePath, maxBytes)
	if err != nil {
		return core.AttachmentData{}, normalizeAttachmentReadError(err)
	}
	return core.AttachmentData{
		Data:     data,
		MIMEType: entry.MIMEType,
		Name:     entry.Title,
		Size:     entry.Size,
	}, nil
}

func normalizeAttachmentReadError(err error) error {
	if errors.Is(err, localstorage.ErrNotFound) {
		return fmt.Errorf("%w: %w", core.ErrAttachmentUnavailable, err)
	}
	return err
}

func defaultStorageRoot(dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = setup.DefaultDBPath()
	}
	if abs, err := filepath.Abs(dbPath); err == nil {
		dbPath = abs
	}
	return filepath.Join(filepath.Dir(dbPath), "storage")
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
