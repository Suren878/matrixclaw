package work

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type ArtifactStore struct {
	root string
}

func NewArtifactStore(root string) *ArtifactStore {
	return &ArtifactStore{root: filepath.Clean(strings.TrimSpace(root))}
}

func (s *ArtifactStore) Write(jobID string, artifactID string, kind string, data []byte) (string, int64, error) {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return "", 0, fmt.Errorf("work: artifact root is not configured")
	}
	jobID = cleanPathToken(jobID)
	artifactID = cleanPathToken(artifactID)
	kind = cleanPathToken(kind)
	if jobID == "" || artifactID == "" || kind == "" {
		return "", 0, fmt.Errorf("work: job id, artifact id, and kind are required")
	}
	dir := filepath.Join(s.root, jobID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, fmt.Errorf("work: create artifact dir: %w", err)
	}
	path := filepath.Join(dir, artifactID+"."+artifactExt(kind))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", 0, fmt.Errorf("work: write artifact: %w", err)
	}
	return path, int64(len(data)), nil
}

var pathTokenPattern = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func cleanPathToken(value string) string {
	value = strings.TrimSpace(value)
	value = pathTokenPattern.ReplaceAllString(value, "_")
	return strings.Trim(value, "._-")
}

func artifactExt(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "html":
		return "html"
	case "text", "extract", "browser_text":
		return "txt"
	case "dom", "snapshot":
		return "dom.txt"
	case "screenshot":
		return "png"
	case "log":
		return "log"
	case "patch":
		return "patch"
	default:
		return "artifact"
	}
}
