package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const binarySniffBytes = 8 * 1024

type grepMatch struct {
	path     string
	modTime  time.Time
	lineNum  int
	charNum  int
	lineText string
}

func (e *grepExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params GrepParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(grepToolName, err)
	}
	if strings.TrimSpace(params.Pattern) == "" {
		return Result{Content: "pattern is required", Status: ResultStatusError}, nil
	}

	policy, pathErr := resolveReadablePath(call.WorkingDir, params.Path)
	if pathErr != nil {
		return *pathErr, nil
	}
	root := policy.Path
	if isProtectedCredentialPath(policy.RealPath) || isProtectedCredentialPath(root) {
		return Result{
			Content:  "Searching MatrixClaw credential files is blocked. Use provider or module status controls; secret values are never returned to model tools.",
			Metadata: filesystemPathMetadata(policy),
			Status:   ResultStatusError,
		}, nil
	}
	pattern := params.Pattern
	if params.LiteralText {
		pattern = regexp.QuoteMeta(pattern)
	}

	matches, truncated, err := grepFiles(ctx, pattern, root, params.Include, defaultSearchLimit)
	if err != nil {
		return Result{}, fmt.Errorf("grep: %w", err)
	}

	if len(matches) == 0 {
		return Result{
			Content: "No files found",
			Metadata: GrepResponseMetadata{
				FilesystemPathMetadata: filesystemPathMetadata(policy),
				NumberOfMatches:        0,
				Truncated:              false,
			},
		}, nil
	}

	var out strings.Builder
	_, _ = fmt.Fprintf(&out, "Found %d matches\n", len(matches))
	currentFile := ""
	for _, match := range matches {
		if currentFile != match.path {
			if currentFile != "" {
				out.WriteString("\n")
			}
			currentFile = match.path
			_, _ = fmt.Fprintf(&out, "%s:\n", filepath.ToSlash(match.path))
		}
		lineText := match.lineText
		if len(lineText) > maxRenderedLineWidth {
			lineText = lineText[:maxRenderedLineWidth] + "..."
		}
		if match.charNum > 0 {
			_, _ = fmt.Fprintf(&out, "  Line %d, Char %d: %s\n", match.lineNum, match.charNum, lineText)
		} else {
			_, _ = fmt.Fprintf(&out, "  Line %d: %s\n", match.lineNum, lineText)
		}
	}
	if truncated {
		out.WriteString("\n(Results are truncated. Consider using a more specific path or pattern.)")
	}

	return Result{
		Content: out.String(),
		Metadata: GrepResponseMetadata{
			FilesystemPathMetadata: filesystemPathMetadata(policy),
			NumberOfMatches:        len(matches),
			Truncated:              truncated,
		},
	}, nil
}

func grepFiles(ctx context.Context, pattern string, root string, include string, limit int) ([]grepMatch, bool, error) {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false, err
	}
	var includeRegex *regexp.Regexp
	if strings.TrimSpace(include) != "" {
		includeRegex, err = globToRegexp(include)
		if err != nil {
			return nil, false, err
		}
	}

	matches := make([]grepMatch, 0, limit)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErrorUnlessRoot(path, root, walkErr)
		}
		if shouldSkipHidden(path, root) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// A link below the root may lead to a file the permission rules guard;
		// the subject they checked is the root.
		if path != root && d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if isProtectedCredentialPath(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if includeRegex != nil && !includeRegex.MatchString(rel) {
			return nil
		}

		var full bool
		matches, full = grepFile(path, regex, matches, limit)
		if full {
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return nil, false, err
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})
	return matches, limit > 0 && len(matches) >= limit, nil
}

// grepFile appends the file's matching lines and reports whether the limit is
// reached. Files larger than read would open, or with a NUL near the start
// (binary), are skipped.
func grepFile(path string, regex *regexp.Regexp, matches []grepMatch, limit int) ([]grepMatch, bool) {
	file, err := os.Open(path)
	if err != nil {
		return matches, false
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxReadBytes {
		return matches, false
	}
	reader := bufio.NewReaderSize(file, binarySniffBytes)
	if head, _ := reader.Peek(binarySniffBytes); bytes.IndexByte(head, 0) >= 0 {
		return matches, false
	}
	for lineNum := 1; ; lineNum++ {
		line, readErr := reader.ReadString('\n')
		if line == "" && readErr != nil {
			return matches, false
		}
		line = strings.TrimSuffix(line, "\n")
		if loc := regex.FindStringIndex(line); loc != nil {
			matches = append(matches, grepMatch{
				path:     path,
				modTime:  info.ModTime(),
				lineNum:  lineNum,
				charNum:  loc[0] + 1,
				lineText: line,
			})
			if limit > 0 && len(matches) >= limit {
				return matches, true
			}
		}
		if readErr != nil {
			return matches, false
		}
	}
}
