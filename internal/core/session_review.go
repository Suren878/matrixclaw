package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ReviewStopReason marks a session review's generation in run_steps; it is
// recorded on the run the review followed and is no step of it.
const ReviewStopReason = "review"

const (
	// reviewRunWindow is how many of a session's newest runs a review reads
	// for its tool steps, its last review and reply-once answers.
	reviewRunWindow = 50
	// reviewTailTokens bounds the conversation a review reads; a third of a
	// smaller window bounds it too.
	reviewTailTokens = 24_000
	// reviewOutputTokens bounds the review's reply.
	reviewOutputTokens = 4_096
)

// SessionReviewSettings are the session review's settings: Enabled turns it
// on, MinToolSteps is the tool-calling steps a session needs since its last
// review, Interval the least time between two reviews of a session.
type SessionReviewSettings struct {
	Enabled      bool
	MinToolSteps int
	Interval     time.Duration
}

// ReviewedSkill is a skill the session uses, shown to its review.
type ReviewedSkill struct {
	ID, Name, Description, Body string
}

// SkillProposal is a skill a session review proposes: a new one, or a new
// version of the session's skill Updates.
type SkillProposal struct {
	Name, Description, Body, Updates string
}

// SkillLearning is the skills library as session reviews use it.
type SkillLearning interface {
	ReviewSettings() SessionReviewSettings
	ReviewedSkills(ctx context.Context, sessionID string) ([]ReviewedSkill, error)
	// DraftProposal files a proposal as a quarantined, disabled draft and
	// returns its id.
	DraftProposal(ctx context.Context, proposal SkillProposal) (string, error)
}

// WithSkillLearning turns on session reviews, which file what they find with
// learning.
func (c *Core) WithSkillLearning(learning SkillLearning) *Core {
	c.reviews.learning = learning
	return c
}

// sessionReviews keeps the reviews of this daemon: the sessions in review and
// when each was last asked for one.
type sessionReviews struct {
	learning SkillLearning
	mu       sync.Mutex
	running  map[string]bool
	asked    map[string]time.Time
}

func (r *sessionReviews) begin(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[sessionID] {
		return false
	}
	if r.running == nil {
		r.running, r.asked = map[string]bool{}, map[string]time.Time{}
	}
	r.running[sessionID] = true
	return true
}

func (r *sessionReviews) end(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, sessionID)
}

// ask notes a review's model call; a call that failed leaves no review in
// run_steps, so this keeps the next try an interval away.
func (r *sessionReviews) ask(sessionID string, at time.Time, interval time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if last, ok := r.asked[sessionID]; ok && at.Sub(last) < interval {
		return false
	}
	r.asked[sessionID] = at
	return true
}

// reviewIdleSession reviews the session in the background when a review is
// due. afterEnd calls it once the session started nothing more; the review
// holds no gate, starts no run and stops with the daemon.
func (c *Core) reviewIdleSession(sessionID string) {
	learning := c.reviews.learning
	if learning == nil || c.lifetime.Err() != nil {
		return
	}
	settings := learning.ReviewSettings()
	if !settings.Enabled || !c.reviews.begin(sessionID) {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		c.reviews.end(sessionID)
		return
	}
	c.executing.Add(1)
	c.mu.Unlock()
	safego.Go("core.reviewSession", func() {
		defer c.executing.Done()
		defer c.reviews.end(sessionID)
		if err := c.reviewSession(c.lifetime, sessionID, settings); err != nil && c.lifetime.Err() == nil {
			log.Printf("core: review of session %q: %v", sessionID, err)
		}
	})
}

// reviewSession asks a model what the session's work since its last review
// teaches, files skill drafts and memory suggestions for the owner to
// approve, and tells the owner once.
func (c *Core) reviewSession(ctx context.Context, sessionID string, settings SessionReviewSettings) error {
	session, runs, since, err := c.dueReview(ctx, sessionID, settings)
	if err != nil || len(runs) == 0 {
		return err
	}
	window, err := c.contextWindow(ctx, session.ID)
	if err != nil {
		return err
	}
	var messages []transcript.Message
	for _, message := range window.Messages {
		if message.CreatedAt.After(since) {
			messages = append(messages, message)
		}
	}
	if len(messages) == 0 {
		return nil
	}
	model, windowTokens := c.compactRuntime(ctx)
	if model == nil {
		if model, _, err = c.resolveSessionRuntime(ctx, c.decorateSessionLLM(session)); err != nil {
			return err
		}
		windowTokens = c.sessionContextWindowTokens(session)
	}
	skills, err := c.reviews.learning.ReviewedSkills(ctx, session.ID)
	if err != nil {
		return err
	}
	remembered, err := c.store.ListMemories(ctx, MemoryFilter{WorkingDir: normalizeWorkingDir(session.WorkingDir), Limit: defaultMemoryLimit})
	if err != nil {
		return err
	}
	tailTokens := reviewTailTokens
	if windowTokens > 0 {
		tailTokens = min(tailTokens, windowTokens/3)
	}
	request := providers.Request{
		SystemPrompt:    reviewSystemPrompt,
		Messages:        []providers.Message{{Role: string(transcript.MessageRoleUser), Content: reviewInput(session, skills, remembered, agentcontext.TranscriptTail(messages, tailTokens))}},
		MaxOutputTokens: reviewOutputTokens,
	}
	if !c.reviews.ask(session.ID, c.now(), settings.Interval) {
		return nil
	}
	response, err := c.generateReview(ctx, runs[0].ID, model, request)
	if err != nil {
		return err
	}
	review, err := parseSessionReview(response.Text, skills, session.WorkingDir)
	if err != nil {
		return fmt.Errorf("the reply was not a review: %w", err)
	}
	drafts, suggested, err := c.fileReview(ctx, review, remembered)
	if err != nil || drafts+suggested == 0 {
		return err
	}
	target, err := c.latestWakeTarget(ctx, runs)
	if err != nil {
		return err
	}
	return c.sendWakeNotice(ctx, wakeNotice{session: session, target: target, text: reviewNoticeText(drafts, suggested)})
}

// dueReview returns the session, its newest runs and the time of its last
// review when a review is due: an idle, top-level native session that
// answered no reply-once address since, past the interval, with enough
// tool-calling steps or a goal completed since. No runs means none is due.
func (c *Core) dueReview(ctx context.Context, sessionID string, settings SessionReviewSettings) (Session, []Run, time.Time, error) {
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil || isSubagentSession(session) || CoreSessionIsExternalAgent(session) {
		return Session{}, nil, time.Time{}, ignoreNotFound(err)
	}
	if active, err := c.sessionRunActive(ctx, session.ID); err != nil || active {
		return Session{}, nil, time.Time{}, err
	}
	if inputs, err := c.store.ListPendingSessionInputs(ctx, session.ID); err != nil || len(inputs) > 0 {
		return Session{}, nil, time.Time{}, err
	}
	runs, err := c.store.ListSessionRuns(ctx, session.ID, reviewRunWindow)
	if err != nil {
		return Session{}, nil, time.Time{}, err
	}
	var since time.Time
	toolSteps := 0
scan:
	for _, run := range runs {
		if c.runRepliesOnce(ctx, run.ID) {
			return Session{}, nil, time.Time{}, nil
		}
		steps, err := c.store.ListRunSteps(ctx, run.ID)
		if err != nil {
			return Session{}, nil, time.Time{}, err
		}
		for i := len(steps) - 1; i >= 0; i-- {
			if steps[i].StopReason == ReviewStopReason {
				since = steps[i].CreatedAt
				break scan
			}
			if steps[i].ToolCalls > 0 {
				toolSteps++
			}
		}
	}
	if !since.IsZero() && c.now().Sub(since) < settings.Interval {
		return Session{}, nil, time.Time{}, nil
	}
	if toolSteps < settings.MinToolSteps {
		goal, err := c.store.GetSessionGoal(ctx, session.ID)
		if errors.Is(err, ErrNotFound) || (err == nil && (goal.Status != GoalComplete || !goal.UpdatedAt.After(since))) {
			return Session{}, nil, time.Time{}, nil
		}
		if err != nil {
			return Session{}, nil, time.Time{}, err
		}
	}
	return session, runs, since, nil
}

// generateReview asks model on a model slot and records the generation on
// runID, as /compact records its summary.
func (c *Core) generateReview(ctx context.Context, runID string, model agent.Model, request providers.Request) (providers.Response, error) {
	release, err := c.modelSlots.Acquire(ctx)
	if err != nil {
		return providers.Response{}, err
	}
	started := time.Now()
	response, err := model.Generate(ctx, request)
	release()
	if err != nil {
		return providers.Response{}, err
	}
	c.recordRunStep(ctx, agent.Step{RunID: runID, Model: response.Model, Provider: response.Provider, Usage: response.Usage, StopReason: ReviewStopReason, Latency: time.Since(started)})
	return response, nil
}

// fileReview files the review's skills as drafts and its memories as pending
// entries, leaving out entries already remembered or suggested.
func (c *Core) fileReview(ctx context.Context, review sessionReview, remembered []MemoryEntry) (drafts int, suggested int, err error) {
	for _, proposal := range review.Skills {
		if _, err := c.reviews.learning.DraftProposal(ctx, proposal); err != nil {
			return drafts, suggested, err
		}
		drafts++
	}
	pending, err := c.store.ListMemories(ctx, MemoryFilter{Pending: true})
	if err != nil {
		return drafts, suggested, err
	}
	known := map[string]bool{}
	for _, entry := range append(remembered, pending...) {
		known[strings.ToLower(entry.Content)] = true
	}
	for _, entry := range review.Memories {
		if known[strings.ToLower(entry.Content)] {
			continue
		}
		known[strings.ToLower(entry.Content)] = true
		entry.Pending = true
		if _, err := c.CreateMemory(ctx, entry); err != nil {
			return drafts, suggested, err
		}
		suggested++
	}
	return drafts, suggested, nil
}

func reviewNoticeText(drafts int, suggested int) string {
	var found, places []string
	if drafts > 0 {
		found = append(found, quantityText(drafts, "skill draft"))
		places = append(places, "skills in /modules skills review")
	}
	if suggested > 0 {
		found = append(found, quantityText(suggested, "memory suggestion"))
		places = append(places, "memory in /memory pending")
	}
	return "Reviewing this session left " + strings.Join(found, " and ") + ". Nothing is used until you approve it: " + strings.Join(places, ", ") + "."
}
