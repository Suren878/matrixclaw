package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// detect prefers bubblewrap, as Codex does, and falls back to Landlock.
func detect() Status {
	var bwrapFailed string
	if path, err := exec.LookPath(Bubblewrap); err == nil {
		base := []string{path, "--ro-bind", "/", "/", "--dev", "/dev"}
		if err := probe(append(base, "--", "true")...); err == nil {
			network := probe(append(base, "--unshare-net", "--", "true")...) == nil
			return Status{Mechanism: Bubblewrap, Network: network, Detail: "bubblewrap (" + path + ")"}
		} else {
			bwrapFailed = "; bwrap does not work: " + err.Error()
		}
	}
	if abi := landlockABI(); abi > 0 {
		return Status{Mechanism: Landlock, Network: abi >= 4, Detail: fmt.Sprintf("Landlock ABI %d", abi)}
	}
	return Status{Detail: "neither bubblewrap (bwrap) nor Landlock (Linux 5.13+ with landlock in its lsm= list) is available" + bwrapFailed}
}

// landlockABI is the Landlock ABI version of the kernel; 0 without Landlock.
func landlockABI() int {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0
	}
	return int(abi)
}

const (
	readAccess = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR
	// fileAccess are the rights a rule on a file rather than a directory may grant.
	fileAccess = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
)

// handledAccess are the file system rights the ABI version can restrict.
func handledAccess(abi int) uint64 {
	access := uint64(unix.LANDLOCK_ACCESS_FS_MAKE_SYM<<1 - 1)
	if abi >= 2 {
		access |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		access |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi >= 5 {
		access |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	return access
}

// RunHelper is the hidden HelperCommand: args are a JSON Policy and the
// command line to run. It confines its own thread and runs the command from
// it, so the command and everything it starts stay confined.
func RunHelper(args []string) int {
	if len(args) < 2 {
		_, _ = fmt.Fprintln(os.Stderr, "matrixclaw sandbox: want a policy and a command")
		return 126
	}
	var p Policy
	if err := json.Unmarshal([]byte(args[0]), &p); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "matrixclaw sandbox: %v\n", err)
		return 126
	}
	// Landlock and no_new_privs bind the calling thread; exec keeps them.
	runtime.LockOSThread()
	if err := restrict(p); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "matrixclaw sandbox: %v\n", err)
		return 126
	}
	path, err := exec.LookPath(args[1])
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "matrixclaw sandbox: %v\n", err)
		return 127
	}
	err = syscall.Exec(path, args[1:], os.Environ())
	_, _ = fmt.Fprintf(os.Stderr, "matrixclaw sandbox: run %s: %v\n", path, err)
	return 126
}

// restrict confines the calling thread to p: it reads everything, writes
// p's directories and the device files and, with the network cut, binds and
// connects no TCP socket.
func restrict(p Policy) error {
	abi := landlockABI()
	if abi < 1 {
		return errors.New("the kernel has no Landlock")
	}
	all := handledAccess(abi)
	attr := unix.LandlockRulesetAttr{Access_fs: all}
	if p.NoNetwork {
		if abi < 4 {
			return fmt.Errorf("the kernel's Landlock ABI %d cannot cut the network", abi)
		}
		attr.Access_net = unix.LANDLOCK_ACCESS_NET_BIND_TCP | unix.LANDLOCK_ACCESS_NET_CONNECT_TCP
	}
	ruleset, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("create the Landlock ruleset: %w", errno)
	}
	fd := int(ruleset)
	defer func() { _ = unix.Close(fd) }()
	if err := allow(fd, "/", readAccess); err != nil {
		return err
	}
	for _, dir := range p.Writable() {
		if err := allow(fd, dir, all); err != nil {
			return err
		}
	}
	for _, file := range devFiles {
		if err := allow(fd, file, all&fileAccess); err != nil {
			return err
		}
	}
	if err := allow(fd, "/dev/pts", all&fileAccess); err != nil {
		return err
	}
	if err := allow(fd, "/dev/shm", all); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, ruleset, 0, 0); errno != 0 {
		return fmt.Errorf("enforce the Landlock ruleset: %w", errno)
	}
	return nil
}

// allow grants access beneath path, which may be missing; a file gets only
// the rights a file can have.
func allow(ruleset int, path string, access uint64) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = unix.Close(fd) }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		access &= fileAccess
	}
	rule := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(fd)}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(ruleset), unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("allow %s: %w", path, errno)
	}
	return nil
}
