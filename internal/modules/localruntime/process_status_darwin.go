//go:build darwin

package localruntime

import (
	"os/exec"
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
	output, err := exec.Command("ps", "-axo", "rss=,comm=,args=").Output()
	if err != nil {
		return 0
	}
	var total uint64
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		rssKiB, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if processFieldsMatchNames(fields[1:], names) {
			total += rssKiB * 1024
		}
	}
	return total
}

func processFieldsMatchNames(fields []string, names []string) bool {
	for _, field := range fields {
		base := filepath.Base(strings.TrimSpace(field))
		for _, name := range names {
			if base == name {
				return true
			}
		}
	}
	return false
}
