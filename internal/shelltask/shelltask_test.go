package shelltask_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/sandbox"
	"github.com/Suren878/matrixclaw/internal/shelltask"
)

func newOutput(t *testing.T) *shelltask.Output {
	t.Helper()
	out, err := shelltask.CreateOutput(filepath.Join(t.TempDir(), "tasks", "task_1.log"))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOutputFileIsPrivateAndReadFromACursor(t *testing.T) {
	out := newOutput(t)
	if _, err := out.Write([]byte("hello\nwörld\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v", info.Mode(), err)
	}

	first, err := shelltask.Read(out.Path(), 0, 8)
	if err != nil {
		t.Fatal(err)
	}
	// "wö" would split the two-byte ö; the read stops before it.
	if first.Text != "hello\nw" || first.Next != 7 || !first.More {
		t.Fatalf("first = %+v", first)
	}
	rest, err := shelltask.Read(out.Path(), first.Next, 100)
	if err != nil || rest.Text != "örld\n" || rest.More {
		t.Fatalf("rest = %+v, %v", rest, err)
	}
}

func TestOutputKeepsItsHeadAndNewestTailPastTheCap(t *testing.T) {
	out := newOutput(t)
	head := bytes.Repeat([]byte("h"), shelltask.HeadBytes)
	middle := bytes.Repeat([]byte("m"), shelltask.MaxBytes)
	for _, part := range [][]byte{head, middle, []byte("THE END")} {
		if _, err := out.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(out.Path())
	if err != nil || info.Size() > shelltask.MaxBytes {
		t.Fatalf("size = %d, %v", info.Size(), err)
	}
	tail, err := shelltask.Tail(out.Path(), 7)
	if err != nil || tail != "THE END" {
		t.Fatalf("tail = %q, %v", tail, err)
	}

	start, err := shelltask.Read(out.Path(), 0, 10)
	if err != nil || start.Text != "hhhhhhhhhh" {
		t.Fatalf("start = %+v, %v", start, err)
	}
	// Reading past the head skips what was dropped and lands in the kept tail.
	afterHead, err := shelltask.Read(out.Path(), shelltask.HeadBytes, 10)
	if err != nil || afterHead.Skipped == 0 || afterHead.Text != "mmmmmmmmmm" {
		t.Fatalf("after head = skipped %d text %q, %v", afterHead.Skipped, afterHead.Text, err)
	}
	total := int64(shelltask.HeadBytes) + shelltask.MaxBytes + 7
	end, err := shelltask.Read(out.Path(), total-7, 100)
	if err != nil || end.Text != "THE END" || end.More || end.Next != total {
		t.Fatalf("end = %+v, %v", end, err)
	}
}

func TestReadingInOrderGetsPastAHeadCutMidCharacter(t *testing.T) {
	out := newOutput(t)
	// One ASCII byte, then two-byte runes: the head cut splits one of them.
	if _, err := out.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	line := []byte(strings.Repeat("я", 1<<15))
	for written := 0; written < shelltask.MaxBytes+shelltask.HeadBytes; written += len(line) {
		if _, err := out.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	var cursor int64
	for range 200 {
		chunk, err := shelltask.Read(out.Path(), cursor, 64<<10)
		if err != nil {
			t.Fatal(err)
		}
		if !chunk.More {
			return
		}
		if chunk.Next == cursor {
			t.Fatalf("stuck at cursor %d: empty chunk with more to read", cursor)
		}
		cursor = chunk.Next
	}
	t.Fatalf("did not reach the end; cursor %d", cursor)
}

func TestProcessRunsInItsOwnGroupAndReportsItsExitCode(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("echo out; echo err >&2; exit 3", t.TempDir(), nil, sandbox.Policy{}, out)
	if err != nil {
		t.Fatal(err)
	}
	if code := p.ExitCode(); code != 3 {
		t.Fatalf("exit code = %d", code)
	}
	chunk, err := shelltask.Read(out.Path(), 0, 100)
	if err != nil || chunk.Text != "out\nerr\n" {
		t.Fatalf("output = %+v, %v", chunk, err)
	}
}

// startReady starts command, which prints "ready" once it set itself up.
func startReady(t *testing.T, command string) *shelltask.Process {
	t.Helper()
	out := newOutput(t)
	p, err := shelltask.Start(command, t.TempDir(), nil, sandbox.Policy{}, out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shelltask.KillGroup(p.PID()) })
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if chunk, _ := shelltask.Read(out.Path(), 0, 100); strings.Contains(chunk.Text, "ready") {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatal("the command never got ready")
		}
	}
}

func waitDone(t *testing.T, p *shelltask.Process) {
	t.Helper()
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the group outlived Stop")
	}
}

func TestStopEndsTheWholeGroup(t *testing.T) {
	p := startReady(t, "sleep 60 & echo ready; sleep 60; wait")
	if err := p.Stop(time.Minute); err != nil {
		t.Fatal(err)
	}
	waitDone(t, p)
	if code := p.ExitCode(); code != -1 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestStopLetsACommandEndOnTERM(t *testing.T) {
	p := startReady(t, "trap 'echo bye; exit 0' TERM; echo ready; while true; do sleep 0.1; done")
	if err := p.Stop(time.Minute); err != nil {
		t.Fatal(err)
	}
	waitDone(t, p)
	chunk, err := shelltask.Read(p.Output().Path(), 0, 100)
	if code := p.ExitCode(); code != 0 || err != nil || !strings.HasSuffix(chunk.Text, "bye\n") {
		t.Fatalf("exit code = %d, output = %q, %v", code, chunk.Text, err)
	}
}

func TestStopKillsACommandThatIgnoresTERM(t *testing.T) {
	p := startReady(t, "trap '' TERM; echo ready; while true; do sleep 0.1; done")
	if err := p.Stop(500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
		t.Fatal("SIGTERM ended a command that ignores it")
	case <-time.After(200 * time.Millisecond):
	}
	waitDone(t, p)
	if code := p.ExitCode(); code != -1 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestKillLeftoverChecksTheLeaderWithoutPS(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("sleep 60", t.TempDir(), nil, sandbox.Policy{}, out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shelltask.KillGroup(p.PID()) })
	t.Setenv("PATH", "/nonexistent")
	leader := p.Leader()
	if leader.PID != p.PID() || leader.BootID == "" || leader.Start == "" {
		t.Fatalf("leader = %+v", leader)
	}

	unknown, otherStart, otherBoot := leader, leader, leader
	unknown.Start, otherStart.Start, otherBoot.BootID = "", "1", "another boot"
	if err := shelltask.KillLeftover(unknown); err == nil {
		t.Fatal("an unknown leader was no error")
	}
	for _, other := range []shelltask.Leader{otherStart, otherBoot} {
		_ = shelltask.KillLeftover(other)
	}
	select {
	case <-p.Done():
		t.Fatal("a group that is not the leftover was killed")
	case <-time.After(200 * time.Millisecond):
	}

	if err := shelltask.KillLeftover(leader); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the leftover group was not killed")
	}
}

func TestKillLeftoverLeavesALeaderlessGroupAlone(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("sleep 60 & exit 0", t.TempDir(), nil, sandbox.Policy{}, out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shelltask.KillGroup(p.PID()) })
	for deadline := time.Now().Add(10 * time.Second); syscall.Kill(p.PID(), 0) == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the shell did not exit")
		}
	}

	if err := shelltask.KillLeftover(p.Leader()); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-p.PID(), 0); err != nil {
		t.Fatalf("the group without its leader was killed: %v", err)
	}
}

func TestReadOfAMissingFileFails(t *testing.T) {
	if _, err := shelltask.Read(filepath.Join(t.TempDir(), "gone.log"), 0, 10); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("err = %v", err)
	}
}

func TestOneWriteLargerThanTheCapKeepsTheHead(t *testing.T) {
	out := newOutput(t)
	start := bytes.Repeat([]byte("s"), 100)
	huge := append(bytes.Repeat([]byte("m"), shelltask.MaxBytes), "THE END"...)
	for _, part := range [][]byte{start, huge} {
		if n, err := out.Write(part); err != nil || n != len(part) {
			t.Fatalf("write = %d, %v", n, err)
		}
	}

	head, err := shelltask.Read(out.Path(), 98, 4)
	if err != nil || head.Text != "ssmm" {
		t.Fatalf("head = %+v, %v", head, err)
	}
	total := int64(len(start) + len(huge))
	end, err := shelltask.Read(out.Path(), total-7, 100)
	if err != nil || end.Text != "THE END" || end.Next != total {
		t.Fatalf("end = %+v, %v", end, err)
	}
}

func TestAFailedCompactionLosesNoCount(t *testing.T) {
	out := newOutput(t)
	full := [][]byte{bytes.Repeat([]byte("h"), shelltask.HeadBytes), bytes.Repeat([]byte("m"), shelltask.MaxBytes-shelltask.HeadBytes)}
	for _, part := range full {
		if _, err := out.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Dir(out.Path())
	if err := os.Rename(dir, dir+".away"); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("lost")); err == nil {
		t.Fatal("a write that could not compact succeeded")
	}
	if err := os.Rename(dir+".away", dir); err != nil {
		t.Fatal(err)
	}

	if _, err := out.Write([]byte("THE END")); err != nil {
		t.Fatal(err)
	}
	total := int64(shelltask.MaxBytes + 7)
	end, err := shelltask.Read(out.Path(), total-7, 100)
	if err != nil || end.Text != "THE END" || end.Next != total || end.More {
		t.Fatalf("end = %+v, %v", end, err)
	}
}

func TestDoneWaitsForCommandsTheShellLeftRunning(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("(sleep 0.5; echo late) & echo early", t.TempDir(), nil, sandbox.Policy{}, out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shelltask.KillGroup(p.PID()) })
	select {
	case <-p.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("the shell did not exit")
	}
	select {
	case <-p.Done():
		t.Fatal("done before the command the shell left running")
	default:
	}
	waitDone(t, p)
	if chunk, err := shelltask.Read(out.Path(), 0, 100); err != nil || chunk.Text != "early\nlate\n" {
		t.Fatalf("output = %+v, %v", chunk, err)
	}
}

func TestWaitOutputStopsReadingAfterTheDelay(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("(sleep 30; echo late) & echo early", t.TempDir(), nil, sandbox.Policy{}, out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shelltask.KillGroup(p.PID()) })

	p.WaitOutput(100 * time.Millisecond)

	waitDone(t, p)
	if chunk, err := shelltask.Read(out.Path(), 0, 100); err != nil || chunk.Text != "early\n" {
		t.Fatalf("output = %+v, %v", chunk, err)
	}
}

func TestProcessGetsOnlyTheEnvironmentItIsGiven(t *testing.T) {
	t.Setenv("SHELLTASK_TEST_SECRET", "sk-leak")
	out := newOutput(t)
	p, err := shelltask.Start("env", t.TempDir(), []string{"PATH=" + os.Getenv("PATH"), "VISIBLE=yes"}, sandbox.Policy{}, out)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, p)
	chunk, err := shelltask.Read(out.Path(), 0, 1<<16)
	if err != nil || !strings.Contains(chunk.Text, "VISIBLE=yes") || strings.Contains(chunk.Text, "sk-leak") {
		t.Fatalf("env output = %q, %v", chunk.Text, err)
	}
}

func TestKillLeftoversEndsWhatTheShellLeftRunning(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("sleep 60 >/dev/null 2>&1 & echo started", t.TempDir(), nil, sandbox.Policy{}, out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shelltask.KillGroup(p.PID()) })
	p.WaitOutput(time.Second)

	if !p.KillLeftovers() {
		t.Fatal("the leftover sleep was not found")
	}
	for deadline := time.Now().Add(10 * time.Second); syscall.Kill(-p.PID(), 0) == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the leftover group still runs")
		}
	}
	if p.KillLeftovers() {
		t.Fatal("a second kill found leftovers")
	}
}

func TestExcerptKeepsHeadAndLongerTailOfLongOutput(t *testing.T) {
	out := newOutput(t)
	var text strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&text, "line %04d\n", i)
	}
	if _, err := out.Write([]byte(text.String())); err != nil {
		t.Fatal(err)
	}

	whole, cut, err := shelltask.Excerpt(out.Path(), 20000)
	if err != nil || cut || whole != text.String() {
		t.Fatalf("short excerpt cut=%v err=%v", cut, err)
	}
	excerpt, cut, err := shelltask.Excerpt(out.Path(), 300)
	if err != nil || !cut {
		t.Fatalf("cut=%v err=%v", cut, err)
	}
	want := "line 0000\nline 0001\nline 0002\nline 0003\nline 0004\nline 0005\nline 0006\nline 0007\nline 0008\nline 0009\n[... 9700 bytes of output left out ...]\n"
	if !strings.HasPrefix(excerpt, want) || !strings.HasSuffix(excerpt, "line 0999\n") || len(excerpt) > 300+len("\n[... 9700 bytes of output left out ...]\n") {
		t.Fatalf("excerpt = %q", excerpt)
	}
}

func TestLoginEnvironmentReadsWhatTheProfileExports(t *testing.T) {
	home := t.TempDir()
	profile := "echo welcome\nexport PROFILE_ADDED=1\n"
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := shelltask.LoginEnvironment([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "KEPT=x"}, 10*time.Second)

	if err != nil || !slices.Contains(env, "PROFILE_ADDED=1") || !slices.Contains(env, "KEPT=x") || slices.ContainsFunc(env, func(pair string) bool { return strings.HasPrefix(pair, "SHLVL=") }) {
		t.Fatalf("env = %q, %v", env, err)
	}
}
