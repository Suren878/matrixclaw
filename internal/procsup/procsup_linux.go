//go:build linux

package procsup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Prepare makes cmd die with the daemon: Linux kills it when the daemon's
// thread that started it exits.
func Prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

func rssBytes(pid int) uint64 {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "statm"))
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
