package shelltask

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// loginMarker separates what profile scripts print from the environment.
const loginMarker = "\x00shelltask-environment\x00"

// shellOwned are variables bash sets for every shell itself.
var shellOwned = map[string]bool{"_": true, "SHLVL": true, "PWD": true, "OLDPWD": true}

// LoginEnvironment is the environment a bash login shell started with environ
// sets up, such as the PATH entries of profile scripts; read once, it lets
// commands run with a plain bash -c, which is faster and prints nothing extra.
func LoginEnvironment(environ []string, timeout time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-lc", `printf '\0shelltask-environment\0'; env -0`)
	cmd.Env = environ
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return KillGroup(cmd.Process.Pid) }
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	_, printed, ok := bytes.Cut(out, []byte(loginMarker))
	if !ok {
		return nil, errors.New("shelltask: the login shell printed no environment")
	}
	var env []string
	for pair := range bytes.SplitSeq(printed, []byte{0}) {
		name, _, ok := bytes.Cut(pair, []byte("="))
		if ok && len(name) > 0 && !shellOwned[string(name)] {
			env = append(env, string(pair))
		}
	}
	return env, nil
}
