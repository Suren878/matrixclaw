package shelltask_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestProcessRunsInItsOwnGroupAndReportsItsExitCode(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("echo out; echo err >&2; exit 3", t.TempDir(), out, time.Second)
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

func TestKillEndsTheWholeGroup(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("sleep 60 & sleep 60; wait", t.TempDir(), out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the group outlived Kill")
	}
	if code := p.ExitCode(); code != -1 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestKillLeftoverChecksTheLeaderWithoutPS(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("sleep 60", t.TempDir(), out, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Kill() })
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
	p, err := shelltask.Start("sleep 60 & exit 0", t.TempDir(), out, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Kill() })
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
