package tools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var credentials struct {
	mu      sync.RWMutex
	sources []func() []string
}

// ProtectCredentials adds the files and directories where an owner (setup,
// a provider, an agent CLI) keeps keys and login tokens. File tools refuse
// them, everything inside them and their backups (setup.json.bak).
func ProtectCredentials(sources ...func() []string) {
	credentials.mu.Lock()
	defer credentials.mu.Unlock()
	credentials.sources = append(credentials.sources, sources...)
}

// IsCredentialPath reports whether file tools refuse path as a protected
// credential place.
func IsCredentialPath(path string) bool {
	path = comparableFilesystemPath(path)
	return path != "" && protectedCredentials()(path)
}

// protectedCredentials resolves the protected places once, for a call that
// checks many paths; the returned check takes a resolved path.
func protectedCredentials() func(real string) bool {
	credentials.mu.RLock()
	sources := credentials.sources
	credentials.mu.RUnlock()
	var places []string
	for _, source := range sources {
		for _, place := range source() {
			if place = comparableFilesystemPath(place); place != "" {
				places = append(places, place)
			}
		}
	}
	return func(real string) bool {
		for _, place := range places {
			if real == place || strings.HasPrefix(real, place+string(filepath.Separator)) ||
				filepath.Dir(real) == filepath.Dir(place) && strings.HasPrefix(filepath.Base(real), filepath.Base(place)+".") {
				return true
			}
		}
		return false
	}
}

// HoldsSecrets reports whether path is in a place where keys and login tokens
// usually live (SSH, GPG, cloud and registry credentials), which a read-only
// tool may touch only after the user agrees.
func HoldsSecrets(path string) bool {
	path = comparableFilesystemPath(path)
	home, err := os.UserHomeDir()
	if path == "" || err != nil || strings.TrimSpace(home) == "" {
		return false
	}
	home = comparableFilesystemPath(home)
	for _, rel := range []string{
		".ssh", ".gnupg", ".aws", ".azure", ".kube", ".config/gcloud", ".config/gh",
		".docker/config.json", ".netrc", ".git-credentials", ".npmrc", ".pypirc",
	} {
		place := filepath.Join(home, filepath.FromSlash(rel))
		if path == place || strings.HasPrefix(path, place+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
