package procsup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as the supervised process.
func TestMain(m *testing.M) {
	switch os.Getenv("PROCSUP_HELPER") {
	case "":
		os.Exit(m.Run())
	case "sleep":
		if path := os.Getenv("PROCSUP_READY_FILE"); path != "" {
			time.Sleep(200 * time.Millisecond)
			_ = os.WriteFile(path, []byte("ok"), 0o644)
		}
		time.Sleep(time.Minute)
	case "exit":
		os.Exit(3)
	case "echo":
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			_ = os.WriteFile(os.Getenv("PROCSUP_OUT"), scanner.Bytes(), 0o644)
		}
	}
	os.Exit(0)
}

func helperSpec(t *testing.T, key string, mode string, env ...string) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{Key: key, Path: exe, Env: append([]string{"PROCSUP_HELPER=" + mode}, env...)}
}

func fileReady(path string) func(context.Context) error {
	return func(context.Context) error {
		_, err := os.Stat(path)
		return err
	}
}

func TestStartReusesTheProcessOfAnEqualSpec(t *testing.T) {
	s := New(t.TempDir())
	defer s.StopAll()
	first, err := s.Start(context.Background(), helperSpec(t, "a", "sleep"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Start(context.Background(), helperSpec(t, "a", "sleep"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !second.Running() {
		t.Fatal("an equal spec started a second process")
	}
}

func TestStartReplacesTheProcessWhenTheSpecChanges(t *testing.T) {
	s := New(t.TempDir())
	defer s.StopAll()
	first, err := s.Start(context.Background(), helperSpec(t, "a", "sleep", "X=1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Start(context.Background(), helperSpec(t, "a", "sleep", "X=2"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first.Running() || !second.Running() {
		t.Fatal("changed spec did not replace the process")
	}
}

func TestStartWaitsForReadinessAndSharesTheProbe(t *testing.T) {
	s := New(t.TempDir())
	defer s.StopAll()
	readyFile := filepath.Join(t.TempDir(), "ready")
	spec := helperSpec(t, "a", "sleep", "PROCSUP_READY_FILE="+readyFile)
	spec.Ready = fileReady(readyFile)
	var wg sync.WaitGroup
	procs := make([]*Process, 3)
	for i := range procs {
		wg.Go(func() {
			p, err := s.Start(context.Background(), spec)
			if err != nil {
				t.Error(err)
			}
			procs[i] = p
		})
	}
	wg.Wait()
	if procs[0] == nil || procs[0] != procs[1] || procs[1] != procs[2] {
		t.Fatal("concurrent starts ran more than one process")
	}
	if _, err := os.Stat(readyFile); err != nil {
		t.Fatal("Start returned before the process was ready")
	}
}

func TestStartStopsAProcessThatExitsBeforeItIsReady(t *testing.T) {
	s := New(t.TempDir())
	spec := helperSpec(t, "a", "exit")
	spec.Ready = func(context.Context) error { return errors.New("not yet") }
	if _, err := s.Start(context.Background(), spec); err == nil {
		t.Fatal("expected an error for a process that exits")
	}
	if _, ok := s.Running("a"); ok {
		t.Fatal("failed process is still registered")
	}
}

func TestStartTimesOutAndStopsTheProcess(t *testing.T) {
	s := New(t.TempDir())
	spec := helperSpec(t, "a", "sleep")
	spec.Ready = func(context.Context) error { return errors.New("never") }
	spec.ReadyTimeout = 300 * time.Millisecond
	if _, err := s.Start(context.Background(), spec); err == nil {
		t.Fatal("expected a readiness timeout")
	}
	if _, ok := s.Running("a"); ok {
		t.Fatal("timed out process is still running")
	}
}

func TestProbeOfOneKeyDoesNotBlockAnother(t *testing.T) {
	s := New(t.TempDir())
	defer s.StopAll()
	slow := helperSpec(t, "slow", "sleep")
	slow.Ready = func(context.Context) error { return errors.New("never") }
	slow.ReadyTimeout = 5 * time.Second
	go func() { _, _ = s.Start(context.Background(), slow) }()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	if _, err := s.Start(context.Background(), helperSpec(t, "fast", "sleep")); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("fast start waited %s for the slow probe", elapsed)
	}
}

func TestStopAllStopsEveryProcess(t *testing.T) {
	s := New(t.TempDir())
	a, err := s.Start(context.Background(), helperSpec(t, "a", "sleep"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Start(context.Background(), helperSpec(t, "b", "sleep"))
	if err != nil {
		t.Fatal(err)
	}
	s.StopAll()
	if a.Running() || b.Running() {
		t.Fatal("StopAll left a process running")
	}
}

func TestUseWritesToStdin(t *testing.T) {
	s := New(t.TempDir())
	defer s.StopAll()
	out := filepath.Join(t.TempDir(), "out")
	spec := helperSpec(t, "a", "echo", "PROCSUP_OUT="+out)
	spec.Stdin = true
	p, err := s.Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Use(func(stdin io.Writer) error {
		_, err := fmt.Fprintln(stdin, "hello")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, _ := os.ReadFile(out); string(data) == "hello" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("process did not receive stdin")
}

func TestStartReturnsAnErrorForAMissingBinary(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Start(context.Background(), Spec{Key: "a", Path: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("expected start error")
	}
}
