package sandbox

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMain lets the test binary act as the Landlock helper, as the daemon does.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == HelperCommand {
		os.Exit(RunHelper(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// landlockDirs makes a workspace, a cache and a directory outside both, away
// from the temporary directories every workspace_write command may write.
func landlockDirs(t *testing.T) (work, cache, outside string) {
	t.Helper()
	if landlockABI() < 1 {
		t.Skip("the kernel has no Landlock")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(wd, "landlock-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	work, cache, outside = filepath.Join(base, "work"), filepath.Join(base, "cache"), filepath.Join(base, "outside")
	for _, dir := range []string{work, cache, outside} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(outside, "kept"), "kept\n")
	return work, cache, outside
}

func runConfined(t *testing.T, p Policy, dir, command string) error {
	t.Helper()
	argv := helperArgs(p, []string{"bash", "-c", command})
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func TestLandlockWorkspaceWriteWritesOnlyItsDirectories(t *testing.T) {
	work, cache, outside := landlockDirs(t)
	p := Settings{Caches: &[]string{cache}}.Policy(Run{WorkingDir: work, Unattended: true})
	tmp := t.TempDir()
	for _, command := range []string{
		"echo ok > inside && mkdir sub && mv inside sub/moved",
		"echo ok > " + filepath.Join(cache, "entry"),
		"echo ok > " + filepath.Join(tmp, "scratch"),
		"echo ok > /dev/null && cat " + filepath.Join(outside, "kept"),
	} {
		if err := runConfined(t, p, work, command); err != nil {
			t.Errorf("%q: %v", command, err)
		}
	}
	for _, command := range []string{
		"echo no > " + filepath.Join(outside, "new"),
		"echo no >> " + filepath.Join(outside, "kept"),
		"rm " + filepath.Join(outside, "kept"),
	} {
		if err := runConfined(t, p, work, command); err == nil {
			t.Errorf("%q ran", command)
		}
	}
	if data, err := os.ReadFile(filepath.Join(outside, "kept")); err != nil || string(data) != "kept\n" {
		t.Fatalf("outside file = %q, %v", data, err)
	}
}

func TestLandlockReadOnlyWritesNeitherTheWorkspaceNorElsewhere(t *testing.T) {
	work, cache, outside := landlockDirs(t)
	p := Settings{Caches: &[]string{cache}, Writable: []string{outside}}.Policy(Run{WorkingDir: work, ReadOnly: true})
	for _, command := range []string{
		"cat " + filepath.Join(outside, "kept") + " > /dev/null",
		"echo scratch > " + filepath.Join(t.TempDir(), "scratch"),
		"echo ok > " + filepath.Join(cache, "entry"),
	} {
		if err := runConfined(t, p, work, command); err != nil {
			t.Errorf("%q: %v", command, err)
		}
	}
	for _, command := range []string{"echo no > inside", "rm " + filepath.Join(outside, "kept")} {
		if err := runConfined(t, p, work, command); err == nil {
			t.Errorf("%q ran", command)
		}
	}
}

func TestLandlockCutsTCPWhenTheNetworkIsOff(t *testing.T) {
	work, _, _ := landlockDirs(t)
	if landlockABI() < 4 {
		t.Skip("Landlock before ABI 4 cannot restrict TCP")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	connect := fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d", listener.Addr().(*net.TCPAddr).Port)
	online := Policy{Mode: WorkspaceWrite, Workspace: []string{work}}
	if err := runConfined(t, online, work, connect); err != nil {
		t.Fatalf("connect with the network on: %v", err)
	}
	offline := online
	offline.NoNetwork = true
	if err := runConfined(t, offline, work, connect); err == nil {
		t.Fatal("connected with the network off")
	}
}
