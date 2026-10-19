package shelltask

import (
	"fmt"
	"syscall"

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

// bootID names the running boot of the system by when it booted.
func bootID() (string, error) {
	booted, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%06d", booted.Sec, booted.Usec), nil
}

// groupAttr starts a command in a process group of its own.
func groupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
