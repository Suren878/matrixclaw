//go:build darwin

package procsup

import (
	"os/exec"
	"strconv"
	"strings"
)

// Prepare is a no-op: macOS has no parent-death signal.
func Prepare(*exec.Cmd) {}

func rssBytes(pid int) uint64 {
	output, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	kib, err := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0
	}
	return kib * 1024
}
