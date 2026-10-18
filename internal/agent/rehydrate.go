package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// After a summary, the rehydrateFiles files edited last are re-attached within
// rehydrateTokens in all (a tenth of a smaller window) and rehydrateFileTokens
// each; a file no longer gets a share smaller than rehydrateMinTokens.
const (
	rehydrateFiles      = 5
	rehydrateTokens     = 8_000
	rehydrateFileTokens = 3_000
	rehydrateMinTokens  = 500
	rehydrateReadBytes  = 256 << 10
)

// rehydrate re-attaches the current content of the files the session edited
// last, as a note after a new boundary, so the model works on from what they
// hold now instead of reading them again.
func (r *run) rehydrate(ctx context.Context) error {
	budget := min(rehydrateTokens, r.contextLimit()/10)
	note := rehydrationNote(recentEdits(r.history.all(), r.task.WorkingDir, rehydrateFiles), budget)
	if note == "" {
		return nil
	}
	return r.appendEngineMessage(ctx, transcript.OriginEngineModel, note)
}

// recentEdits lists the paths of up to limit files the messages changed with a
// successful edit-tool call, newest first, resolved against workingDir.
func recentEdits(messages []transcript.Message, workingDir string, limit int) []string {
	succeeded := map[string]bool{}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil && !part.ToolResult.IsError() {
				succeeded[strings.TrimSpace(part.ToolResult.ToolCallID)] = true
			}
		}
	}
	var paths []string
	seen := map[string]bool{}
	for i := len(messages) - 1; i >= 0 && len(paths) < limit; i-- {
		parts := messages[i].Parts
		for j := len(parts) - 1; j >= 0 && len(paths) < limit; j-- {
			call := parts[j].ToolCall
			if call == nil || !editTools[call.Name] || !succeeded[strings.TrimSpace(call.ID)] {
				continue
			}
			var params struct {
				FilePath string `json:"file_path"`
			}
			if json.Unmarshal([]byte(call.Input), &params) != nil || strings.TrimSpace(params.FilePath) == "" {
				continue
			}
			policy, err := tools.ResolveFilesystemPath(workingDir, params.FilePath)
			if err != nil || seen[policy.Path] {
				continue
			}
			seen[policy.Path] = true
			paths = append(paths, policy.Path)
		}
	}
	return paths
}

// rehydrationNote holds the current text of paths within budget tokens; files
// gone, binary or in a secrets location are left out.
func rehydrationNote(paths []string, budget int) string {
	var b strings.Builder
	for _, path := range paths {
		if budget < rehydrateMinTokens {
			break
		}
		text, ok := currentText(path)
		if !ok {
			continue
		}
		text = agentcontext.HeadTail(text, min(rehydrateFileTokens, budget))
		budget -= agentcontext.EstimateTextTokens(text)
		b.WriteString("\n\n<file path=\"" + path + "\">\n" + text + "\n</file>")
	}
	if b.Len() == 0 {
		return ""
	}
	return "Files edited most recently in this session, as they are now (re-attached after the summary; read a file again only for a part left out here):" + b.String()
}

// currentText is the text of the regular file at path, read up to
// rehydrateReadBytes.
func currentText(path string) (string, bool) {
	if tools.HoldsSecrets(path) {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	data, err := io.ReadAll(io.LimitReader(file, rehydrateReadBytes))
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	// The read limit may have split the last rune.
	for cut := 0; cut < utf8.UTFMax-1 && len(data) > 0 && !utf8.Valid(data); cut++ {
		data = data[:len(data)-1]
	}
	return string(data), utf8.Valid(data)
}
