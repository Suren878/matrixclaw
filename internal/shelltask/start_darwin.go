package shelltask

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// processStart is when process pid started, as the kernel has it.
func processStart(pid int) (string, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	if int(info.Proc.P_pid) != pid {
		return "", fmt.Errorf("shelltask: no process %d", pid)
	}
	start := info.Proc.P_starttime
	return fmt.Sprintf("%d.%06d", start.Sec, start.Usec), nil
}
