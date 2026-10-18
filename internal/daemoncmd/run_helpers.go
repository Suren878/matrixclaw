package daemoncmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/core"
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

// promptEnvironment is where native runs are told they work: the daemon
// timezone and the user's AGENTS.md next to setup.json.
func promptEnvironment(bootstrap bootstrapConfig) core.PromptEnvironment {
	env := core.PromptEnvironment{}
	if location, err := time.LoadLocation(bootstrap.Timezone); err == nil {
		env.Timezone = location
	}
	if bootstrap.SetupService != nil {
		env.UserInstructions = filepath.Join(filepath.Dir(bootstrap.SetupService.Path()), "AGENTS.md")
	}
	return env
}
