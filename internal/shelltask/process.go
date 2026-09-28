package shelltask

import (
	"errors"
	"os/exec"
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
	leader    Leader
}

// Leader identifies a process group's leader across daemon restarts: the boot
// it ran in and its start within it tell it apart from a later process with
// its PID. Empty fields could not be read.
type Leader struct {
	PID    int
	BootID string
	Start  string
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
	// Until Wait reaps the shell its PID cannot go to another process.
	p.leader = Leader{PID: p.PID()}
	p.leader.BootID, _ = bootID()
	p.leader.Start, _ = processStart(p.PID())
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

// Leader identifies the shell, the leader of the process group.
func (p *Process) Leader() Leader {
	return p.leader
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

// KillLeftover kills the process group a daemon that is gone started. The
// group is left alone when its leader is gone, is another process that got its
// PID, or cannot be checked; its processes then outlive the task.
func KillLeftover(leader Leader) error {
	if leader.PID <= 0 || leader.BootID == "" || leader.Start == "" {
		return errors.New("shelltask: the leader is unknown")
	}
	boot, err := bootID()
	if err != nil {
		return err
	}
	if boot != leader.BootID {
		return nil
	}
	if err := syscall.Kill(leader.PID, 0); errors.Is(err, syscall.ESRCH) {
		return nil
	}
	start, err := processStart(leader.PID)
	if err != nil {
		return err
	}
	if start != leader.Start {
		return nil
	}
	if pgid, err := syscall.Getpgid(leader.PID); err != nil || pgid != leader.PID {
		return err
	}
	return KillGroup(leader.PID)
}
