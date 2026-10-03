package realtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type fakeCore struct{ CoreBridge }

func (fakeCore) CreateSession(context.Context, core.CreateSessionInput) (core.Session, error) {
	return core.Session{ID: "core-1"}, nil
}

func (fakeCore) GetSession(context.Context, string) (core.Session, error) {
	return core.Session{ID: "core-1"}, nil
}

func (fakeCore) ListToolSpecs() []tools.Spec { return nil }

type failingProvider struct{}

func (failingProvider) Descriptor(context.Context) ProviderDescriptor {
	return ProviderDescriptor{ID: ProviderGemini, Config: ProviderConfigSummary{ModelID: "model"}}
}

func (failingProvider) Connect(context.Context, ProviderConnectRequest) (ProviderConnection, error) {
	return nil, errors.New("provider down")
}

type discardStream struct{}

func (discardStream) Read(ctx context.Context) (Event, error) {
	<-ctx.Done()
	return Event{}, ctx.Err()
}
func (discardStream) Write(context.Context, Event) error { return nil }
func (discardStream) Close(error) error                  { return nil }

func newTestManager(clock *time.Time) *Manager {
	m := NewManager(fakeCore{}, Config{Enabled: true, MaxSessions: 1}, failingProvider{})
	m.now = func() time.Time { return *clock }
	return m
}

func TestFinishedStreamFreesSessionSlot(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	m := newTestManager(&clock)
	first, err := m.CreateSession(ctx, SessionCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ServeStream(ctx, first.ID, discardStream{}); err == nil {
		t.Fatal("expected provider error")
	}
	if _, err := m.Session(ctx, first.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("finished session still listed: %v", err)
	}
	if _, err := m.CreateSession(ctx, SessionCreateRequest{}); err != nil {
		t.Fatalf("slot not freed: %v", err)
	}
}

func TestClosedSessionIsForgotten(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	m := newTestManager(&clock)
	first, err := m.CreateSession(ctx, SessionCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := m.CloseSession(ctx, first.ID)
	if err != nil || closed.Status != SessionStatusClosed {
		t.Fatalf("close = %+v, %v", closed, err)
	}
	if _, err := m.Session(ctx, first.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("closed session still listed: %v", err)
	}
}

func TestUnstreamedSessionExpires(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	m := newTestManager(&clock)
	if _, err := m.CreateSession(ctx, SessionCreateRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateSession(ctx, SessionCreateRequest{}); err == nil {
		t.Fatal("expected session limit")
	}
	clock = clock.Add(2 * unstreamedSessionTTL)
	if _, err := m.CreateSession(ctx, SessionCreateRequest{}); err != nil {
		t.Fatalf("stale session still holds the slot: %v", err)
	}
}
