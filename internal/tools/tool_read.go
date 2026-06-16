package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func (e *readExecutor) Execute(_ context.Context, call Call) (Result, error) {
	var params ReadParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(readToolName, err)
	}
	if strings.TrimSpace(params.FilePath) == "" {
		return Result{Content: "file_path is required", IsError: true}, nil
	}

	policy, pathErr := resolveReadablePath(call.WorkingDir, params.FilePath)
	if pathErr != nil {
		return *pathErr, nil
	}
	path := policy.Path
	info, err := os.Stat(path)
	if err != nil {
		return Result{Content: fmt.Sprintf("File not found: %s", path), Metadata: filesystemPathMetadata(policy), IsError: true}, nil
	}
	if info.IsDir() {
		return Result{Content: fmt.Sprintf("Path is a directory, not a file: %s", path), Metadata: filesystemPathMetadata(policy), IsError: true}, nil
	}
	if info.Size() > maxReadBytes {
		return Result{Content: fmt.Sprintf("File is too large (%d bytes)", info.Size()), Metadata: filesystemPathMetadata(policy), IsError: true}, nil
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultReadLimit
	}
	content, hasMore, err := readTextFile(path, params.Offset, limit)
	if err != nil {
		return Result{}, fmt.Errorf("read: read file: %w", err)
	}

	body := "<file>\n" + addLineNumbers(content, params.Offset+1)
	if hasMore {
		body += fmt.Sprintf("\n\n(File has more lines. Use offset to read beyond line %d)", params.Offset+len(strings.Split(content, "\n")))
	}
	body += "\n</file>"

	return Result{
		Content: body,
		Metadata: ReadResponseMetadata{
			FilesystemPathMetadata: filesystemPathMetadata(policy),
			FilePath:               path,
			Content:                content,
		},
	}, nil
}

func readTextFile(path string, offset int, limit int) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineIndex := 0
	lines := []string{}
	hasMore := false
	for scanner.Scan() {
		if lineIndex < offset {
			lineIndex++
			continue
		}
		if len(lines) >= limit {
			hasMore = true
			break
		}
		lines = append(lines, scanner.Text())
		lineIndex++
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	return strings.Join(lines, "\n"), hasMore, nil
}

func addLineNumbers(content string, start int) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return ""
	}
	var out strings.Builder
	for i, line := range lines {
		_, _ = fmt.Fprintf(&out, "%6d\t%s", start+i, line)
		if i != len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
