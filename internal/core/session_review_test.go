package core_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/memory"
	skillsmodule "github.com/Suren878/matrixclaw/internal/modules/skills"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const reviewReply = `{"skills":[{"name":"Go race tests","description":"Use when testing Go code","body":"Run go test -race on the changed packages first."}],"memories":[{"scope":"user","content":"The user wants commit messages in English."}]}`

// reviewHarness is a store with the skills library, on which each core sees
// sessions end; the compact model writes the reviews.
type reviewHarness struct {
	t      *testing.T
	db     *store.SQLiteStore
	skills *skillsmodule.Module
	reply  string
	asked  atomic.Int32
}

func newReviewHarness(t *testing.T, cfg setup.SkillsConfig) *reviewHarness {
	t.Helper()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	module, err := skillsmodule.New(db.DB(), filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Apply(context.Background(), setup.Config{Modules: setup.ModulesConfig{Skills: cfg}}); err != nil {
		t.Fatal(err)
	}
	return &reviewHarness{t: t, db: db, skills: module, reply: reviewReply}
}

// core is a daemon at now; call stop before the next one starts.
func (h *reviewHarness) core(now time.Time) *core.Core {
	app := core.New(core.Config{Store: h.db, Clock: func() time.Time { return now }, SkillLearning: h.skills, Memory: memory.New(h.db, nil)})
	app.SetTools(tools.NewRegistry())
	app.SetSessionLLMs(twoModelLLMs{
		recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
			return providers.Response{Text: "done"}, nil
		})},
		cheap: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
			h.asked.Add(1)
			return providers.Response{Text: h.reply, Usage: providers.Usage{PromptTokens: 900, OutputTokens: 80}}, nil
		}),
	})
	app.SetTuning(core.Tuning{CompactProvider: "cheap", CompactModel: "small"})
	return app
}

func (h *reviewHarness) session(id string, change func(*core.Session)) core.Session {
	h.t.Helper()
	now := runRecoveryTestTime()
	session := core.Session{ID: id, Title: id, Kind: core.SessionKindAssistant, WorkingDir: "/work/app", ProviderID: "recovery-test", ModelID: "test-model", PermissionMode: core.PermissionModeDefault, Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now}
	if change != nil {
		change(&session)
	}
	if err := h.db.CreateSession(context.Background(), session); err != nil {
		h.t.Fatal(err)
	}
	return session
}

// run accepts a run of session at at, with toolSteps model steps that called
// tools recorded on it already.
func (h *reviewHarness) run(session core.Session, id string, status core.RunStatus, at time.Time, toolSteps int) core.Run {
	h.t.Helper()
	ctx := context.Background()
	run := core.Run{ID: id, SessionID: session.ID, UserMessageID: "msg_" + id, Status: status, StartedAt: at, UpdatedAt: at}
	user := transcript.Message{ID: run.UserMessageID, SessionID: session.ID, RunID: id, Role: transcript.MessageRoleUser, Content: "task " + id, Parts: transcript.NormalizeMessageParts("task "+id, nil), CreatedAt: at, UpdatedAt: at}
	if err := h.db.AcceptMessage(ctx, user, run); err != nil {
		h.t.Fatal(err)
	}
	for range toolSteps {
		if err := h.db.SaveRunStep(ctx, core.RunStep{RunID: id, ToolCalls: 1, CreatedAt: at}); err != nil {
			h.t.Fatal(err)
		}
	}
	return run
}

// end lets app execute the run, or cancel it while it waits, then waits for
// what followed.
func (h *reviewHarness) end(app *core.Core, run core.Run) {
	h.t.Helper()
	var err error
	if run.Status == core.RunStatusAccepted {
		err = app.ExecuteRun(context.Background(), run.ID)
	} else {
		_, err = app.CancelRun(context.Background(), run.ID)
	}
	if err != nil {
		h.t.Fatal(err)
	}
	app.WaitRuns()
}

func (h *reviewHarness) reviewSteps(runIDs ...string) int {
	h.t.Helper()
	count := 0
	for _, id := range runIDs {
		steps, err := h.db.ListRunSteps(context.Background(), id)
		if err != nil {
			h.t.Fatal(err)
		}
		for _, step := range steps {
			if step.StopReason == core.ReviewStopReason {
				count++
			}
		}
	}
	return count
}

func TestSessionReviewFilesQuarantinedDraftsAndPendingMemory(t *testing.T) {
	t.Parallel()
	h := newReviewHarness(t, setup.SkillsConfig{})
	session := h.session("s", nil)
	start := runRecoveryTestTime()
	run := h.run(session, "r1", core.RunStatusAccepted, start, 10)
	app := h.core(start.Add(time.Minute))

	h.end(app, run)

	ctx := context.Background()
	if h.asked.Load() != 1 || h.reviewSteps(run.ID) != 1 {
		t.Fatalf("asked = %d, review steps = %d; want one review recorded on the run", h.asked.Load(), h.reviewSteps(run.ID))
	}
	drafts, err := h.skills.Service().List(ctx, skills.SearchOptions{IncludeQuarantined: true, IncludeDisabled: true})
	if err != nil || len(drafts) != 1 {
		t.Fatalf("skills = %+v err = %v", drafts, err)
	}
	if draft := drafts[0]; draft.TrustState != skills.TrustQuarantine || draft.Enabled || draft.Description != "Use when testing Go code" || !slices.Contains(draft.Tags, "learned") {
		t.Fatalf("draft = %+v, want a quarantined, disabled draft tagged learned", draft)
	}
	if usable, _ := h.skills.Service().List(ctx, skills.SearchOptions{}); len(usable) != 0 {
		t.Fatalf("runs can use %+v", usable)
	}
	memories := memory.New(h.db, nil)
	pending, err := memories.List(ctx, memory.Filter{Pending: true})
	if err != nil || len(pending) != 1 || pending[0].Content != "The user wants commit messages in English." {
		t.Fatalf("pending memory = %+v err = %v", pending, err)
	}
	if approved, _ := memories.List(ctx, memory.Filter{}); len(approved) != 0 || promptMemory(t, memories, session.WorkingDir) != "" {
		t.Fatalf("runs see unapproved memory: %+v", approved)
	}
	if notices := systemNotices(t, h.db, session.ID); len(notices) != 1 || !strings.Contains(notices[0], "/modules skills section review") || !strings.Contains(notices[0], "/memory pending") {
		t.Fatalf("notices = %q", notices)
	}

	if _, err := memories.Approve(ctx, pending[0].ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(promptMemory(t, memories, session.WorkingDir), "commit messages in English") {
		t.Fatal("an approved suggestion is not remembered")
	}
}

func TestSessionReviewWaitsForEnoughToolSteps(t *testing.T) {
	t.Parallel()
	h := newReviewHarness(t, setup.SkillsConfig{Review: setup.SkillsReviewConfig{MinToolSteps: 4}})
	session := h.session("s", nil)
	start := runRecoveryTestTime()
	first := h.run(session, "r1", core.RunStatusWaitingApproval, start, 3)

	h.end(h.core(start.Add(time.Minute)), first)
	if h.asked.Load() != 0 {
		t.Fatalf("reviewed after 3 tool steps of 4")
	}

	second := h.run(session, "r2", core.RunStatusWaitingApproval, start.Add(2*time.Minute), 1)
	h.end(h.core(start.Add(3*time.Minute)), second)
	if h.asked.Load() != 1 || h.reviewSteps(first.ID, second.ID) != 1 {
		t.Fatalf("asked = %d after 4 tool steps across two runs, want 1", h.asked.Load())
	}
}

func TestSessionReviewRunsOncePerIntervalAcrossRestarts(t *testing.T) {
	t.Parallel()
	h := newReviewHarness(t, setup.SkillsConfig{})
	session := h.session("s", nil)
	start := runRecoveryTestTime()
	runs := []string{}
	review := func(id string, at time.Time) {
		t.Helper()
		run := h.run(session, id, core.RunStatusWaitingApproval, at, 10)
		runs = append(runs, run.ID)
		h.end(h.core(at.Add(time.Minute)), run)
	}

	review("r1", start)
	review("r2", start.Add(time.Hour))
	if h.asked.Load() != 1 {
		t.Fatalf("asked = %d within the interval, want 1", h.asked.Load())
	}
	review("r3", start.Add(7*time.Hour))
	if h.asked.Load() != 2 || h.reviewSteps(runs...) != 2 {
		t.Fatalf("asked = %d after the interval, want 2", h.asked.Load())
	}
	if pending, _ := h.db.ListMemories(context.Background(), memory.Filter{Pending: true}); len(pending) != 1 {
		t.Fatalf("the same suggestion was filed twice: %+v", pending)
	}
}

func TestSessionReviewFollowsACompletedGoal(t *testing.T) {
	t.Parallel()
	h := newReviewHarness(t, setup.SkillsConfig{})
	session := h.session("s", nil)
	start := runRecoveryTestTime()
	run := h.run(session, "r1", core.RunStatusWaitingApproval, start, 2)
	if err := h.db.SaveSessionGoal(context.Background(), core.SessionGoal{SessionID: session.ID, Objective: "ship", MaxRuns: 5, Status: core.GoalComplete, CreatedAt: start, UpdatedAt: start}); err != nil {
		t.Fatal(err)
	}

	h.end(h.core(start.Add(time.Minute)), run)

	if h.asked.Load() != 1 {
		t.Fatalf("asked = %d after the goal completed, want 1", h.asked.Load())
	}
}

func TestSessionReviewIgnoresAReplyThatIsNoReview(t *testing.T) {
	t.Parallel()
	h := newReviewHarness(t, setup.SkillsConfig{})
	h.reply = "I found nothing worth keeping."
	session := h.session("s", nil)
	start := runRecoveryTestTime()
	run := h.run(session, "r1", core.RunStatusWaitingApproval, start, 10)

	h.end(h.core(start.Add(time.Minute)), run)

	all, _ := h.skills.Service().List(context.Background(), skills.SearchOptions{IncludeQuarantined: true, IncludeDisabled: true})
	pending, _ := h.db.ListMemories(context.Background(), memory.Filter{Pending: true})
	if h.asked.Load() != 1 || h.reviewSteps(run.ID) != 1 || len(all) != 0 || len(pending) != 0 || len(systemNotices(t, h.db, session.ID)) != 0 {
		t.Fatalf("asked = %d skills = %+v pending = %+v notices = %q", h.asked.Load(), all, pending, systemNotices(t, h.db, session.ID))
	}
}

func TestSessionReviewSkipsSessionsItMustNotLearnFrom(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		cfg     setup.SkillsConfig
		session func(*core.Session)
		run     func(*reviewHarness, core.Run)
	}{
		"review off": {cfg: setup.SkillsConfig{Review: setup.SkillsReviewConfig{Enabled: new(false)}}},
		"skills off": {cfg: setup.SkillsConfig{Enabled: new(false)}},
		"subagent":   {session: func(s *core.Session) { s.ParentSessionID, s.Hidden = "parent", true }},
		"external":   {session: func(s *core.Session) { s.Kind, s.ExternalAgentID = core.SessionKindExternalAgent, "codex-app" }},
		"reply once": {run: func(h *reviewHarness, run core.Run) {
			if err := h.db.CreateClientDelivery(context.Background(), core.ClientDelivery{ID: "d1", Type: core.ClientDeliveryTypeRun, Client: "telegram", ExternalKey: "guest", SessionID: run.SessionID, RunID: run.ID, ReplyOnce: true, Status: core.ClientDeliveryStatusPending, CreatedAt: run.StartedAt, UpdatedAt: run.StartedAt}); err != nil {
				h.t.Fatal(err)
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newReviewHarness(t, tc.cfg)
			session := h.session("s", tc.session)
			start := runRecoveryTestTime()
			run := h.run(session, "r1", core.RunStatusWaitingApproval, start, 20)
			if tc.run != nil {
				tc.run(h, run)
			}

			h.end(h.core(start.Add(time.Minute)), run)

			if h.asked.Load() != 0 || h.reviewSteps(run.ID) != 0 {
				t.Fatalf("%s session was reviewed", name)
			}
		})
	}
}

// systemNotices are the texts of the notices shown in the session.
func systemNotices(t *testing.T, db *store.SQLiteStore, sessionID string) []string {
	t.Helper()
	messages, err := db.ListMessages(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var notices []string
	for _, message := range messages {
		if message.Role == transcript.MessageRoleSystem && message.Origin == "" {
			notices = append(notices, message.Content)
		}
	}
	return notices
}

func promptMemory(t *testing.T, memories *memory.Service, workingDir string) string {
	t.Helper()
	text, err := memories.PromptContext(context.Background(), workingDir)
	if err != nil {
		t.Fatal(err)
	}
	return text
}
