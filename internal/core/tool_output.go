package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// WithSessionFiles sets the directory that holds per-session files such as full
// tool outputs; a session's files are removed with it.
func (c *Core) WithSessionFiles(root string) *Core {
	c.sessionFiles = strings.TrimSpace(root)
	return c
}

// keepLargeOutput writes a result too large for the model's context to a file of
// the session and leaves the model its head and tail with the file's path.
func (c *Core) keepLargeOutput(sessionID string, result tools.Result) tools.Result {
	tokens := agentcontext.EstimateTextTokens(result.Content)
	if tokens <= agentcontext.LargeOutputTokens {
		return result
	}
	excerpt := agentcontext.HeadTail(result.Content, agentcontext.LargeOutputTokens-200)
	path, err := c.writeToolOutput(sessionID, result.Content)
	if err != nil {
		log.Printf("core: keep large tool output of session %q: %v", sessionID, err)
		result.Content = excerpt
		return result
	}
	result.OutputPath = path
	how := "read it with offset and limit, or grep it"
	if len(result.Content) > tools.MaxReadBytes {
		how = "too large for read; grep it, or view parts with head and tail"
	}
	result.Content = fmt.Sprintf("Output is ~%s tokens; the full output is in %s (%s).\n\n%s", agentcontext.FormatShortNumber(tokens), path, how, excerpt)
	return result
}

// toolFailure is the error result of a tool call that failed with err.
func (c *Core) toolFailure(sessionID string, err error) tools.Result {
	return c.keepLargeOutput(sessionID, tools.Result{Content: err.Error(), Status: tools.ResultStatusError})
}

// toolOutputDir holds a session's kept tool outputs.
const toolOutputDir = "tool-output"

// writeToolOutput stores content under the session, named by its hash so a
// repeated output lands in the same file.
func (c *Core) writeToolOutput(sessionID string, content string) (string, error) {
	sessionDir, err := c.sessionDir(sessionID)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(sessionDir, toolOutputDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(content))
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+".txt")
	file, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return "", err
	}
	_, writeErr := file.WriteString(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return path, nil
}

// pruneToolOutputs deletes the session's kept tool outputs except those the
// messages after seq refer to; files still being written are left alone.
func (c *Core) pruneToolOutputs(ctx context.Context, sessionID string, seq int64) error {
	sessionDir, err := c.sessionDir(sessionID)
	if err != nil {
		return nil
	}
	dir := filepath.Join(sessionDir, toolOutputDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	later, err := c.store.ListMessagesAfter(ctx, sessionID, seq, 0)
	if err != nil {
		return err
	}
	referenced := map[string]bool{}
	for _, message := range later {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.OutputPath != "" {
				referenced[part.ToolResult.OutputPath] = true
			}
		}
	}
	var errs []error
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if strings.HasPrefix(entry.Name(), ".") || referenced[path] {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// removeSessionFiles deletes the files kept for a session.
func (c *Core) removeSessionFiles(sessionID string) error {
	dir, err := c.sessionDir(sessionID)
	if err != nil {
		return nil
	}
	return os.RemoveAll(dir)
}

// sessionDir is the session's directory under the session files root; an ID
// that is not a plain path element has none.
func (c *Core) sessionDir(sessionID string) (string, error) {
	if c.sessionFiles == "" {
		return "", errors.New("no session files directory is configured")
	}
	if sessionID == "" || strings.IndexFunc(sessionID, func(r rune) bool {
		return r != '-' && r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	}) >= 0 {
		return "", fmt.Errorf("session id %q cannot name a directory", sessionID)
	}
	return filepath.Join(c.sessionFiles, sessionID), nil
}
