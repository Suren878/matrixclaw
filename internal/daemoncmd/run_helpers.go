package daemoncmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
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

// removeWebResearchFiles deletes the page files the retired web research jobs
// kept in <data dir>/web-research; a symlink or a file there is left alone.
func removeWebResearchFiles(dbPath string) {
	dir := filepath.Join(dataDir(dbPath), "web-research")
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	files := 0
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files++
		}
		return nil
	})
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("matrixclawd: remove retired web research files: %v", err)
		return
	}
	log.Printf("matrixclawd: removed %s (%d retired web research files)", dir, files)
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
