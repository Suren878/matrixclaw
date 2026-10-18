package shelltask

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// Ran is what a command Run waited for printed, cut at its limit, and its exit code.
type Ran struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Run runs command with bash -c in dir and env (nil inherits the daemon's), in
// a process group of its own, with stdin as its input, and waits for it. When
// ctx stops first its group is killed and ctx's error returned. Processes it
// leaves in its group are killed; each output keeps at most limit bytes.
func Run(ctx context.Context, command, dir string, env []string, stdin []byte, limit int) (Ran, error) {
	stdout, stderr := &cappedBuffer{limit: limit}, &cappedBuffer{limit: limit}
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir, cmd.Env = dir, env
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return KillGroup(cmd.Process.Pid) }
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if cmd.Process != nil {
		_ = KillGroup(cmd.Process.Pid)
	}
	ran := Ran{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: -1}
	if ctx.Err() != nil {
		return ran, ctx.Err()
	}
	if cmd.ProcessState != nil {
		ran.ExitCode = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	if err == nil || errors.As(err, &exitErr) || errors.Is(err, exec.ErrWaitDelay) {
		return ran, nil
	}
	return ran, err
}

// cappedBuffer keeps the first limit bytes written to it and drops the rest.
type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
