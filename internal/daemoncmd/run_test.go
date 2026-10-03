package daemoncmd

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestRunCLIStopsOnSIGTERM(t *testing.T) {
	var out bytes.Buffer
	code := RunCLI(context.Background(), &out, &out, "matrixclawd", nil, func(ctx context.Context) error {
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
			t.Error("SIGTERM did not cancel the daemon context")
			return nil
		}
	})
	if code != 0 {
		t.Fatalf("RunCLI = %d, output %q", code, out.String())
	}
}

func TestRunShutsDownWithActiveRunAndOpenStream(t *testing.T) {
	modelCalled := make(chan struct{}, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case modelCalled <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer model.Close()

	dir := t.TempDir()
	addr := freeLoopbackAddr(t)
	dbPath := filepath.Join(dir, "state", "matrixclaw.db")
	setupPath := filepath.Join(dir, "setup.json")
	err := setup.NewFileStore(setupPath).Save(setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "fake",
		Providers: []setup.ProviderConfig{{
			ID: "fake", Name: "Fake", Type: "openai-compatible", APIKey: "sk-test", BaseURL: model.URL, Model: "fake-model",
		}},
		Daemon: setup.DaemonConfig{HTTPAddr: addr, DBPath: dbPath, APIToken: "test-token"},
	})
	if err != nil {
		t.Fatalf("save setup: %v", err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "xdg-state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)
	t.Setenv("MATRIXCLAW_HTTP_ADDR", "")
	t.Setenv("MATRIXCLAW_DB_PATH", "")
	t.Setenv("MATRIXCLAW_API_TOKEN", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx) }()

	client := daemonclient.New("http://"+addr, "test", "test:1").WithAPIToken("test-token")
	waitForDaemon(t, client, done)
	session, err := client.CreateSession(context.Background(), "shutdown", dir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	openEventStream(t, addr, session.ID)
	accepted, err := client.SendMessage(context.Background(), session.ID, "hello", dir)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	select {
	case <-modelCalled:
	case <-time.After(10 * time.Second):
		t.Fatal("run never reached the model")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v after shutdown", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return within 15s of shutdown")
	}
	unlock, err := lockDataDir(dataDir(dbPath))
	if err != nil {
		t.Fatalf("data lock still held after shutdown: %v", err)
	}
	_ = unlock()
	assertRunRecoverable(t, dbPath, accepted.Run.ID)
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().String()
}

func waitForDaemon(t *testing.T, client *daemonclient.Client, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("Run exited during startup: %v", err)
		default:
		}
		if _, err := client.Health(context.Background()); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("daemon did not become healthy")
}

func openEventStream(t *testing.T, addr string, sessionID string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/v1/events?session_id="+sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if _, err := bufio.NewReader(resp.Body).ReadString('\n'); err != nil {
		t.Fatalf("read event stream: %v", err)
	}
}

func assertRunRecoverable(t *testing.T, dbPath string, runID string) {
	t.Helper()
	sqliteStore, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = sqliteStore.Close() }()
	run, err := sqliteStore.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	switch run.Status {
	case core.RunStatusCompleted, core.RunStatusFailed, core.RunStatusCanceled:
		t.Fatalf("run status after shutdown = %s, want it kept for recovery", run.Status)
	}
}
