package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

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
		return Result{Content: "pattern is required", IsError: true}, nil
	}

	policy, pathErr := resolveReadablePath(call.WorkingDir, params.Path)
	if pathErr != nil {
		return *pathErr, nil
	}
	root := policy.Path
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
	fmt.Fprintf(&out, "Found %d matches\n", len(matches))
	currentFile := ""
	for _, match := range matches {
		if currentFile != match.path {
			if currentFile != "" {
				out.WriteString("\n")
			}
			currentFile = match.path
			fmt.Fprintf(&out, "%s:\n", filepath.ToSlash(match.path))
		}
		lineText := match.lineText
		if len(lineText) > maxRenderedLineWidth {
			lineText = lineText[:maxRenderedLineWidth] + "..."
		}
		if match.charNum > 0 {
			fmt.Fprintf(&out, "  Line %d, Char %d: %s\n", match.lineNum, match.charNum, lineText)
		} else {
			fmt.Fprintf(&out, "  Line %d: %s\n", match.lineNum, lineText)
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

func grepFiles(_ context.Context, pattern string, root string, include string, limit int) ([]grepMatch, bool, error) {
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
		if walkErr != nil {
			return walkErr
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
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if includeRegex != nil && !includeRegex.MatchString(rel) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(content), "\n")
		info, _ := d.Info()
		for lineIndex, line := range lines {
			loc := regex.FindStringIndex(line)
			if loc == nil {
				continue
			}
			matches = append(matches, grepMatch{
				path:     path,
				modTime:  fileModTime(info),
				lineNum:  lineIndex + 1,
				charNum:  loc[0] + 1,
				lineText: line,
			})
			if limit > 0 && len(matches) >= limit {
				return errStopWalk
			}
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return nil, false, err
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})
	return matches, limit > 0 && len(matches) >= limit, nil
}

func fileModTime(info fs.FileInfo) time.Time {
	if info == nil {
		return time.Time{}
	}
	return info.ModTime()
}
