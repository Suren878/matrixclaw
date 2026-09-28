package shelltask

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Process is a running shell command; its process group has the ID of its PID.
type Process struct {
	cmd       *exec.Cmd
	out       *Output
	reader    *os.File
	exited    chan struct{}
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
// stdout and stderr going to out until every process that has them, the shell
// and the commands it left running, closed them.
func Start(command, dir string, out *Output) (*Process, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout, cmd.Stderr = writer, writer
	p := &Process{cmd: cmd, out: out, reader: reader, exited: make(chan struct{}), done: make(chan struct{}), exitCode: -1, startedAt: time.Now()}
	err = cmd.Start()
	_ = writer.Close()
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	// Until Wait reaps the shell its PID cannot go to another process.
	p.leader = Leader{PID: p.PID()}
	p.leader.BootID, _ = bootID()
	p.leader.Start, _ = processStart(p.PID())
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, reader)
		_ = reader.Close()
		close(copied)
	}()
	go p.wait(copied)
	return p, nil
}

func (p *Process) wait(copied <-chan struct{}) {
	_ = p.cmd.Wait()
	if state := p.cmd.ProcessState; state != nil {
		p.exitCode = state.ExitCode()
	}
	close(p.exited)
	<-copied
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

// Exited is closed once the shell exited; commands it left running may still
// write.
func (p *Process) Exited() <-chan struct{} {
	return p.exited
}

// WaitOutput waits for the shell to exit and then at most delay for the
// commands it left running to close the output; after that it stops reading
// what they write. It returns once Done is closed.
func (p *Process) WaitOutput(delay time.Duration) {
	<-p.exited
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-p.done:
		return
	case <-timer.C:
	}
	_ = p.reader.SetReadDeadline(time.Now())
	<-p.done
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

// Stop asks every process of the group to end with SIGTERM and kills the
// group with SIGKILL when the command is not done within grace.
func (p *Process) Stop(grace time.Duration) error {
	select {
	case <-p.done:
		return nil
	default:
	}
	if err := signalGroup(p.PID(), syscall.SIGTERM); err != nil {
		return err
	}
	go func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-p.done:
		case <-timer.C:
			_ = KillGroup(p.PID())
		}
	}()
	return nil
}

// KillGroup sends SIGKILL to the process group; a gone group is no error.
func KillGroup(pgid int) error {
	return signalGroup(pgid, syscall.SIGKILL)
}

func signalGroup(pgid int, signal syscall.Signal) error {
	if pgid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pgid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
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
