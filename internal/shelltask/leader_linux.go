package shelltask

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// processStart is when process pid started, in clock ticks since boot, as
// /proc/<pid>/stat has it.
func processStart(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	// The command name is in parentheses and may hold anything; the fields
	// after it start at the third, and starttime is the 22nd.
	text := string(data)
	fields := strings.Fields(text[strings.LastIndexByte(text, ')')+1:])
	if len(fields) < 20 {
		return "", fmt.Errorf("shelltask: unreadable /proc/%d/stat", pid)
	}
	return fields[19], nil
}

// bootID names the running boot of the system.
func bootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(data)), err
}
