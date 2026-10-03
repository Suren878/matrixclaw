package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	surfaceinput "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/input"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestMessageWhileBusySteersUnlessQueuedExplicitly(t *testing.T) {
	var modes []core.BusyInputMode
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input core.HandleMessageInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		modes = append(modes, input.BusyMode)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(core.AcceptRunResult{SessionID: "session_1", Status: core.AcceptRunStatusSteered})
	}))
	defer server.Close()
	m := newApp(context.Background(), New(Config{BaseURL: server.URL}))
	m.read = readmodel.New(core.ClientSnapshot{SessionID: "session_1"})

	for _, content := range []string{"also check the logs", "/queue then run the tests"} {
		m.setBusy(true)
		m.handleSubmit(surfaceinput.SubmitMsg{Content: content})()
	}

	if len(modes) != 2 || modes[0] != core.BusyInputModeSteer || modes[1] != core.BusyInputModeQueue {
		t.Fatalf("busy modes = %q", modes)
	}
}

func TestSlashTextThatIsNoCommandIsSentAsAMessage(t *testing.T) {
	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input core.HandleMessageInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sent = append(sent, input.Text)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(core.AcceptRunResult{SessionID: "session_1", Status: core.AcceptRunStatusQueued})
	}))
	defer server.Close()
	m := newApp(context.Background(), New(Config{BaseURL: server.URL}))
	m.read = readmodel.New(core.ClientSnapshot{SessionID: "session_1"})

	contents := []string{"/etc/nginx/nginx.conf fails to load, why?", "/busybox ls prints nothing"}
	for _, content := range contents {
		m.setBusy(true)
		cmd := m.handleSubmit(surfaceinput.SubmitMsg{Content: content})
		if cmd == nil {
			t.Fatalf("%q was dropped", content)
		}
		cmd()
	}

	if len(sent) != 2 || sent[0] != contents[0] || sent[1] != contents[1] {
		t.Fatalf("sent = %q", sent)
	}
}
