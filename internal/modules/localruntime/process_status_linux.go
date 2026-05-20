//go:build linux

package localruntime

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func voiceRuntimeRSSBytes(provider setup.VoiceProviderOption) uint64 {
	names := voiceRuntimeProcessNames(provider)
	if len(names) == 0 {
		return 0
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	var total uint64
	for _, entry := range entries {
		if !entry.IsDir() || !isProcessID(entry.Name()) {
			continue
		}
		if !procMatchesVoiceRuntime(entry.Name(), names) {
			continue
		}
		total += procRSSBytes(entry.Name())
	}
	return total
}

func isProcessID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func procMatchesVoiceRuntime(pid string, names []string) bool {
	if processNameMatches(filepath.Join("/proc", pid, "comm"), names) {
		return true
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil || len(cmdline) == 0 {
		return false
	}
	parts := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		base := filepath.Base(strings.TrimSpace(part))
		for _, name := range names {
			if base == name {
				return true
			}
		}
	}
	return false
}

func processNameMatches(path string, names []string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	base := filepath.Base(strings.TrimSpace(string(content)))
	for _, name := range names {
		if base == name {
			return true
		}
	}
	return false
}

func procRSSBytes(pid string) uint64 {
	data, err := os.ReadFile(filepath.Join("/proc", pid, "statm"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}
