package shelltask

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Process is a running shell command; its process group has the ID of its PID.
type Process struct {
	cmd       *exec.Cmd
	out       *Output
	done      chan struct{}
	exitCode  int
	startedAt time.Time
}

// Start runs command with bash -lc in dir, in a process group of its own, its
// stdout and stderr going to out. Once the shell exits, Done waits at most
// waitDelay for commands it left running to close the output; 0 waits for them.
func Start(command, dir string, out *Output, waitDelay time.Duration) (*Process, error) {
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = waitDelay
	p := &Process{cmd: cmd, out: out, done: make(chan struct{}), exitCode: -1, startedAt: time.Now()}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go p.wait()
	return p, nil
}

func (p *Process) wait() {
	_ = p.cmd.Wait()
	if state := p.cmd.ProcessState; state != nil {
		p.exitCode = state.ExitCode()
	}
	_ = p.out.Close()
	close(p.done)
}

// PID is the shell's process ID, which is also its process group ID.
func (p *Process) PID() int {
	return p.cmd.Process.Pid
}

// StartedAt is when the process was started.
func (p *Process) StartedAt() time.Time {
	return p.startedAt
}

// Output is where the process writes.
func (p *Process) Output() *Output {
	return p.out
}

// Done is closed once the process exited and its output is closed.
func (p *Process) Done() <-chan struct{} {
	return p.done
}

// ExitCode is the shell's exit code once Done is closed; -1 when a signal ended it.
func (p *Process) ExitCode() int {
	<-p.done
	return p.exitCode
}

// Kill ends every process of the group.
func (p *Process) Kill() error {
	return KillGroup(p.PID())
}

// KillGroup sends SIGKILL to the process group; a gone group is no error.
func KillGroup(pgid int) error {
	if pgid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// KillLeftover kills the process group a daemon that is gone started at
// startedAt with leader pid. A leader that is alive but started at another time
// is some other process that got the ID, and is left alone.
func KillLeftover(pid, pgid int, startedAt time.Time) error {
	started, alive, err := processStart(pid)
	if err != nil {
		return err
	}
	if alive && !sameSecond(started, startedAt) {
		return nil
	}
	return KillGroup(pgid)
}

// processStart reads when process pid started, from ps so that it works
// without /proc; alive is false when there is no such process.
func processStart(pid int) (started time.Time, alive bool, err error) {
	if pid <= 0 {
		return time.Time{}, false, nil
	}
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return time.Time{}, false, nil
	}
	cmd := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	out, err := cmd.Output()
	text := strings.Join(strings.Fields(string(out)), " ")
	if text == "" {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	started, err = time.Parse("Mon Jan 2 15:04:05 2006", text)
	return started, true, err
}

// sameSecond reports whether ps's second-precision start time matches t.
func sameSecond(ps time.Time, t time.Time) bool {
	diff := ps.Sub(t.UTC().Truncate(time.Second))
	return diff >= -2*time.Second && diff <= 2*time.Second
}
