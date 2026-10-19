package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// Mechanisms that confine commands.
const (
	Bubblewrap = "bwrap"
	Landlock   = "landlock"
	Seatbelt   = "seatbelt"
)

// Status says how this system confines commands.
type Status struct {
	// Mechanism is Bubblewrap, Landlock or Seatbelt; empty when none works.
	Mechanism string
	// Network reports whether it can cut commands off the network.
	Network bool
	// Detail names its version, or says why none works.
	Detail string
}

// Text is s in a line.
func (s Status) Text() string {
	if s.Mechanism == "" {
		return "unavailable: " + s.Detail
	}
	network := "network can be cut"
	if !s.Network {
		network = "network cannot be cut"
	}
	return s.Detail + ", " + network
}

// Detect finds how this system confines commands, once.
var Detect = sync.OnceValue(detect)

// ErrUnavailable is returned for a command whose sandbox this system cannot apply.
var ErrUnavailable = errors.New("the shell sandbox cannot be applied")

// Resolve is p as this system applies it: p itself when it can confine it,
// the zero Policy when it cannot and p is not strict, and ErrUnavailable when
// it cannot and p is strict or cuts the network.
func (p Policy) Resolve() (Policy, error) {
	if p.Mode != WorkspaceWrite && p.Mode != ReadOnly {
		return Policy{}, nil
	}
	status := Detect()
	switch {
	case status.Mechanism == "" && !p.Strict && !p.NoNetwork:
		return Policy{}, nil
	case status.Mechanism == "":
		return Policy{}, fmt.Errorf("%w: it is set to %s, but %s; install bubblewrap or set daemon.sandbox.mode to off", ErrUnavailable, p.Mode, status.Detail)
	case p.NoNetwork && !status.Network:
		return Policy{}, fmt.Errorf("%w: daemon.sandbox.network is false, but %s cannot cut commands off the network", ErrUnavailable, status.Detail)
	}
	return p, nil
}

// Wrap returns the command line that runs argv under p, a Policy Resolve
// returned.
func (p Policy) Wrap(argv []string) []string {
	if p.Mode != WorkspaceWrite && p.Mode != ReadOnly {
		return argv
	}
	switch Detect().Mechanism {
	case Bubblewrap:
		return bwrapArgs(p, argv)
	case Landlock:
		return helperArgs(p, argv)
	case Seatbelt:
		return seatbeltArgs(p, argv)
	}
	return argv
}

// HelperCommand is the hidden daemon subcommand that confines itself with
// Landlock and runs the command after it.
const HelperCommand = "__sandbox"

// helperExecutable is the running daemon binary, even once an update
// replaced the file.
const helperExecutable = "/proc/self/exe"

func helperArgs(p Policy, argv []string) []string {
	encoded, _ := json.Marshal(p)
	return append([]string{helperExecutable, HelperCommand, string(encoded)}, argv...)
}

// devFiles are the device files every confined command may write.
var devFiles = []string{"/dev/null", "/dev/zero", "/dev/full", "/dev/random", "/dev/urandom", "/dev/tty", "/dev/ptmx"}

func bwrapArgs(p Policy, argv []string) []string {
	args := []string{Bubblewrap, "--ro-bind", "/", "/", "--dev", "/dev"}
	for _, dir := range parentsFirst(p.Writable()) {
		args = append(args, "--bind", dir, dir)
	}
	if p.NoNetwork {
		args = append(args, "--unshare-net")
	}
	return append(append(args, "--"), argv...)
}

// parentsFirst orders dirs so that a bind mount never hides a later one.
func parentsFirst(dirs []string) []string {
	return slices.SortedFunc(slices.Values(dirs), func(a, b string) int { return len(a) - len(b) })
}

func seatbeltArgs(p Policy, argv []string) []string {
	return append([]string{"/usr/bin/sandbox-exec", "-p", seatbeltProfile(p)}, argv...)
}

// seatbeltProfile allows everything but writes outside p's directories and,
// with the network cut, networking.
func seatbeltProfile(p Policy) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n(allow file-write*")
	for _, file := range devFiles {
		fmt.Fprintf(&b, " (literal %s)", sbplString(file))
	}
	b.WriteString(` (subpath "/dev/fd") (regex #"^/dev/ttys[0-9]+$")`)
	for _, dir := range p.Writable() {
		fmt.Fprintf(&b, " (subpath %s)", sbplString(dir))
	}
	b.WriteString(")\n")
	if p.NoNetwork {
		b.WriteString("(deny network*)\n")
	}
	return b.String()
}

func sbplString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// probe runs a mechanism's no-op command line and reports whether it worked.
func probe(argv ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil && len(out) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return err
}
