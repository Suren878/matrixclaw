package workspace

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxInstructionBytes caps the instruction files of one run together.
const MaxInstructionBytes = 32 << 10

// Each directory gives the first of primaryInstructionNames that exists, and
// matrixclaw's own file besides.
var (
	primaryInstructionNames = []string{"AGENTS.md", "CLAUDE.md"}
	ownInstructionName      = filepath.Join(".matrixclaw", "instructions.md")
)

// InstructionFile is one instruction file as a run gets it. Truncated marks
// a file cut, or left out entirely, at MaxInstructionBytes.
type InstructionFile struct {
	Path      string
	Content   string
	Truncated bool
}

// Instructions reads the instruction files for workingDir, outermost first:
// global, then each directory from the top down to workingDir. The top is
// home when workingDir lies under it, else the git root, else workingDir.
func Instructions(workingDir, gitRoot, home, global string) []InstructionFile {
	workingDir = filepath.Clean(workingDir)
	if !filepath.IsAbs(workingDir) {
		return nil
	}
	var paths []string
	if global != "" {
		paths = append(paths, filepath.Clean(global))
	}
	for _, dir := range dirsDown(instructionTop(workingDir, gitRoot, home), workingDir) {
		for _, name := range primaryInstructionNames {
			if path := filepath.Join(dir, name); isFile(path) {
				paths = append(paths, path)
				break
			}
		}
		paths = append(paths, filepath.Join(dir, ownInstructionName))
	}
	return readInstructions(paths)
}

func instructionTop(workingDir, gitRoot, home string) string {
	for _, top := range []string{home, gitRoot} {
		if top != "" && within(filepath.Clean(top), workingDir) {
			return filepath.Clean(top)
		}
	}
	return workingDir
}

func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// dirsDown lists top and every directory below it down to dir.
func dirsDown(top, dir string) []string {
	rel, err := filepath.Rel(top, dir)
	if err != nil || rel == "." {
		return []string{dir}
	}
	dirs := []string{top}
	current := top
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		dirs = append(dirs, current)
	}
	return dirs
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// readInstructions reads the existing, non-empty files among paths within
// the shared byte budget, each path once.
func readInstructions(paths []string) []InstructionFile {
	var files []InstructionFile
	seen := map[string]bool{}
	budget := MaxInstructionBytes
	for _, path := range paths {
		if seen[path] || !isFile(path) {
			continue
		}
		seen[path] = true
		if budget == 0 {
			files = append(files, InstructionFile{Path: path, Truncated: true})
			continue
		}
		content, truncated, err := readCapped(path, budget)
		if err != nil || !truncated && strings.TrimSpace(content) == "" {
			continue
		}
		budget -= len(content)
		if truncated {
			budget = 0
		}
		files = append(files, InstructionFile{Path: path, Content: content, Truncated: truncated})
	}
	return files
}

// readCapped reads at most limit bytes of path, cut after the last whole
// line when the file is longer.
func readCapped(path string, limit int) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return "", false, err
	}
	if len(data) <= limit {
		return strings.ToValidUTF8(string(data), ""), false, nil
	}
	data = data[:bytes.LastIndexByte(data[:limit], '\n')+1]
	return strings.ToValidUTF8(string(data), ""), true, nil
}
