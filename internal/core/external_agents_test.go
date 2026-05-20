package core

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func TestCreateExternalAgentAttachmentStoresCanonicalAgentID(t *testing.T) {
	runtime := &externalAgentTestRuntime{
		id:              "codex-app",
		aliases:         []string{"codex"},
		startedAgentID:  "codex",
		externalThread:  "thread_1",
		externalSession: "session_1",
	}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	store := &externalAgentAttachmentStore{}
	app := New(nil).WithExternalAgents(registry, store)

	err = app.createExternalAgentAttachment(context.Background(), Session{
		ID:             "session_1",
		RuntimeID:      SessionRuntimeExternalAgent,
		WorkingDir:     "/tmp/work",
		PermissionMode: PermissionModeFullAuto,
	}, CreateSessionInput{
		RuntimeID: SessionRuntime("codex"),
		ModelID:   "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("createExternalAgentAttachment() error = %v", err)
	}
	if store.saved.AgentID != "codex-app" {
		t.Fatalf("saved AgentID = %q, want canonical codex-app", store.saved.AgentID)
	}
	if store.saved.ExternalThreadID != "thread_1" || store.saved.ExternalSessionID != "session_1" {
		t.Fatalf("saved attachment = %#v, want external session IDs", store.saved)
	}
}

type externalAgentAttachmentStore struct {
	saved externalagents.SessionAttachment
}

func (s *externalAgentAttachmentStore) SaveExternalAgentSession(_ context.Context, attachment externalagents.SessionAttachment) error {
	s.saved = attachment
	return nil
}

func (s *externalAgentAttachmentStore) GetExternalAgentSession(context.Context, string) (externalagents.SessionAttachment, error) {
	return s.saved, nil
}

func (s *externalAgentAttachmentStore) DeleteExternalAgentSession(context.Context, string) error {
	s.saved = externalagents.SessionAttachment{}
	return nil
}

type externalAgentTestRuntime struct {
	id              string
	aliases         []string
	startedAgentID  string
	externalThread  string
	externalSession string
}

func (r *externalAgentTestRuntime) ID() string { return r.id }

func (r *externalAgentTestRuntime) DisplayName() string { return r.id }

func (r *externalAgentTestRuntime) Aliases() []string { return r.aliases }

func (r *externalAgentTestRuntime) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true}
}

func (r *externalAgentTestRuntime) StartSession(context.Context, externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	return externalagents.ExternalSession{
		AgentID:           r.startedAgentID,
		ExternalThreadID:  r.externalThread,
		ExternalSessionID: r.externalSession,
	}, nil
}

func (r *externalAgentTestRuntime) ResumeSession(context.Context, externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	return externalagents.ExternalSession{}, nil
}

func (r *externalAgentTestRuntime) Send(context.Context, externalagents.ExternalSession, externalagents.Input) (<-chan externalagents.Event, error) {
	return nil, nil
}

func (r *externalAgentTestRuntime) Interrupt(context.Context, externalagents.ExternalSession) error {
	return nil
}

func (r *externalAgentTestRuntime) Close() error { return nil }
