package webresearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/work"
)

const webResearchJobKindPrefix = work.KindWebResearch + "."

var ErrNotFound = errors.New("webresearch: not found")

type WorkStore struct {
	store work.Store
	close func() error
}

type researchPayload struct {
	Query       string   `json:"query,omitempty"`
	Answer      string   `json:"answer,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	NextActions []string `json:"next_actions,omitempty"`
	Sources     []Source `json:"sources,omitempty"`
}

func NewSQLiteStore(path string) (*WorkStore, error) {
	store, err := work.NewSQLiteStore(path)
	if err != nil {
		return nil, fmt.Errorf("webresearch: open work store: %w", err)
	}
	return &WorkStore{store: store, close: store.Close}, nil
}

func NewStore(store work.Store) *WorkStore {
	if store == nil {
		return nil
	}
	return &WorkStore{store: store}
}

func (s *WorkStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func (s *WorkStore) CreateSession(ctx context.Context, session ResearchSession) error {
	payload := researchPayload{
		Query:       session.Query,
		Answer:      session.Answer,
		Warnings:    session.Warnings,
		NextActions: session.NextActions,
	}
	job := work.Job{
		ID:         session.ID,
		Kind:       work.KindWebResearch,
		Status:     normalizeStatus(session.Status),
		Task:       session.Task,
		Summary:    session.Summary,
		ResultJSON: marshalPayload(payload),
		CreatedAt:  session.CreatedAt,
		UpdatedAt:  session.UpdatedAt,
		FinishedAt: session.CompletedAt,
	}
	if err := s.store.CreateJob(ctx, job); err != nil {
		return fmt.Errorf("webresearch: create research work job: %w", err)
	}
	return nil
}

func (s *WorkStore) UpdateSession(ctx context.Context, session ResearchSession) error {
	job, err := s.store.GetJob(ctx, strings.TrimSpace(session.ID))
	if err != nil {
		return mapWorkErr("get research work job", err)
	}
	payload := payloadFromJob(job)
	payload.Query = session.Query
	payload.Answer = session.Answer
	payload.Warnings = session.Warnings
	payload.NextActions = session.NextActions
	job.Status = normalizeStatus(session.Status)
	job.Task = session.Task
	job.Summary = session.Summary
	job.ResultJSON = marshalPayload(payload)
	job.UpdatedAt = session.UpdatedAt
	job.FinishedAt = session.CompletedAt
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return mapWorkErr("update research work job", err)
	}
	return nil
}

func (s *WorkStore) GetSession(ctx context.Context, id string) (ResearchSession, error) {
	job, err := s.store.GetJob(ctx, strings.TrimSpace(id))
	if err != nil {
		return ResearchSession{}, mapWorkErr("get research work job", err)
	}
	if job.Kind != work.KindWebResearch {
		return ResearchSession{}, ErrNotFound
	}
	payload := payloadFromJob(job)
	return ResearchSession{
		ID:          job.ID,
		Task:        job.Task,
		Query:       payload.Query,
		Status:      normalizeStatus(job.Status),
		Answer:      payload.Answer,
		Summary:     job.Summary,
		Warnings:    payload.Warnings,
		NextActions: payload.NextActions,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		CompletedAt: job.FinishedAt,
	}, nil
}

func (s *WorkStore) CreateJob(ctx context.Context, job ResearchJob) error {
	workJob := work.Job{
		ID:          job.ID,
		Kind:        workKindForResearchJob(job.Kind),
		Status:      normalizeStatus(job.Status),
		Task:        taskFromInputJSON(job.InputJSON),
		InputJSON:   job.InputJSON,
		Error:       job.Error,
		Attempts:    job.Attempts,
		WorkerRef:   strings.TrimSpace(job.ResearchID),
		HeartbeatAt: job.HeartbeatAt,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		FinishedAt:  job.FinishedAt,
	}
	if err := s.store.CreateJob(ctx, workJob); err != nil {
		return fmt.Errorf("webresearch: create work child job: %w", err)
	}
	return nil
}

func (s *WorkStore) UpdateJob(ctx context.Context, job ResearchJob) error {
	workJob, err := s.store.GetJob(ctx, strings.TrimSpace(job.ID))
	if err != nil {
		return mapWorkErr("get work child job", err)
	}
	workJob.Kind = workKindForResearchJob(job.Kind)
	workJob.Status = normalizeStatus(job.Status)
	workJob.Task = taskFromInputJSON(job.InputJSON)
	workJob.InputJSON = job.InputJSON
	workJob.Error = job.Error
	workJob.Attempts = job.Attempts
	workJob.WorkerRef = strings.TrimSpace(job.ResearchID)
	workJob.HeartbeatAt = job.HeartbeatAt
	workJob.UpdatedAt = job.UpdatedAt
	workJob.FinishedAt = job.FinishedAt
	if workJob.StartedAt == nil && job.Status == StatusRunning {
		started := job.UpdatedAt
		workJob.StartedAt = &started
	}
	if err := s.store.UpdateJob(ctx, workJob); err != nil {
		return mapWorkErr("update work child job", err)
	}
	return nil
}

func (s *WorkStore) GetJob(ctx context.Context, id string) (ResearchJob, error) {
	job, err := s.store.GetJob(ctx, strings.TrimSpace(id))
	if err != nil {
		return ResearchJob{}, mapWorkErr("get work child job", err)
	}
	return researchJobFromWork(job), nil
}

func (s *WorkStore) LatestJob(ctx context.Context, researchID string) (ResearchJob, error) {
	jobs, err := s.store.ListJobs(ctx, work.JobFilter{
		KindPrefix: webResearchJobKindPrefix,
		WorkerRef:  strings.TrimSpace(researchID),
	})
	if err != nil {
		return ResearchJob{}, fmt.Errorf("webresearch: latest work child job: %w", err)
	}
	if len(jobs) == 0 {
		return ResearchJob{}, ErrNotFound
	}
	return researchJobFromWork(jobs[len(jobs)-1]), nil
}

func (s *WorkStore) ListStaleJobs(ctx context.Context, before time.Time, retryLimit int) ([]ResearchJob, error) {
	jobs, err := s.store.ListJobs(ctx, work.JobFilter{
		KindPrefix: webResearchJobKindPrefix,
		Statuses:   []string{StatusRunning},
	})
	if err != nil {
		return nil, fmt.Errorf("webresearch: list stale work jobs: %w", err)
	}
	var out []ResearchJob
	for _, job := range jobs {
		if job.Attempts > retryLimit {
			continue
		}
		last := job.UpdatedAt
		if job.HeartbeatAt != nil {
			last = *job.HeartbeatAt
		}
		if last.Before(before) {
			out = append(out, researchJobFromWork(job))
		}
	}
	return out, nil
}

func (s *WorkStore) ListRunnableJobs(ctx context.Context, limit int) ([]ResearchJob, error) {
	jobs, err := s.store.ListJobs(ctx, work.JobFilter{
		KindPrefix: webResearchJobKindPrefix,
		Statuses:   []string{StatusPending},
		Limit:      limit,
	})
	if err != nil {
		return nil, fmt.Errorf("webresearch: list runnable work jobs: %w", err)
	}
	out := make([]ResearchJob, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, researchJobFromWork(job))
	}
	return out, nil
}

func (s *WorkStore) UpsertSource(ctx context.Context, source Source) error {
	source.ResearchID = strings.TrimSpace(source.ResearchID)
	job, err := s.store.GetJob(ctx, source.ResearchID)
	if err != nil {
		return mapWorkErr("get research source payload", err)
	}
	payload := payloadFromJob(job)
	replaced := false
	for i := range payload.Sources {
		if payload.Sources[i].ID == source.ID {
			payload.Sources[i] = source
			replaced = true
			break
		}
	}
	if !replaced {
		payload.Sources = append(payload.Sources, source)
	}
	job.ResultJSON = marshalPayload(payload)
	if source.UpdatedAt.After(job.UpdatedAt) {
		job.UpdatedAt = source.UpdatedAt
	}
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return mapWorkErr("update research source payload", err)
	}
	return nil
}

func (s *WorkStore) ListSources(ctx context.Context, researchID string) ([]Source, error) {
	job, err := s.store.GetJob(ctx, strings.TrimSpace(researchID))
	if err != nil {
		return nil, mapWorkErr("get research sources", err)
	}
	payload := payloadFromJob(job)
	return append([]Source(nil), payload.Sources...), nil
}

func (s *WorkStore) ReplaceFacts(ctx context.Context, researchID string, facts []Fact) error {
	workFacts := make([]work.Fact, 0, len(facts))
	for _, fact := range facts {
		workFacts = append(workFacts, work.Fact{
			ID:         fact.ID,
			JobID:      strings.TrimSpace(researchID),
			Claim:      fact.Claim,
			Confidence: fact.Confidence,
			SourceIDs:  fact.SourceIDs,
			CreatedAt:  fact.CreatedAt,
		})
	}
	if err := s.store.ReplaceFacts(ctx, researchID, workFacts); err != nil {
		return fmt.Errorf("webresearch: replace work facts: %w", err)
	}
	return nil
}

func (s *WorkStore) ListFacts(ctx context.Context, researchID string) ([]Fact, error) {
	workFacts, err := s.store.ListFacts(ctx, strings.TrimSpace(researchID))
	if err != nil {
		return nil, fmt.Errorf("webresearch: list work facts: %w", err)
	}
	facts := make([]Fact, 0, len(workFacts))
	for _, fact := range workFacts {
		facts = append(facts, Fact{
			ID:         fact.ID,
			ResearchID: fact.JobID,
			Claim:      fact.Claim,
			SourceIDs:  fact.SourceIDs,
			Confidence: fact.Confidence,
			CreatedAt:  fact.CreatedAt,
		})
	}
	return facts, nil
}

func (s *WorkStore) CreateArtifact(ctx context.Context, artifact Artifact) error {
	workArtifact := work.Artifact{
		ID:        artifact.ID,
		JobID:     artifact.ResearchID,
		SourceID:  artifact.SourceID,
		Kind:      artifact.Kind,
		Path:      artifact.Path,
		MIMEType:  artifact.MIMEType,
		ByteCount: artifact.ByteCount,
		CreatedAt: artifact.CreatedAt,
	}
	if err := s.store.CreateArtifact(ctx, workArtifact); err != nil {
		return fmt.Errorf("webresearch: create work artifact: %w", err)
	}
	return nil
}

func (s *WorkStore) ListArtifacts(ctx context.Context, researchID string) ([]Artifact, error) {
	workArtifacts, err := s.store.ListArtifacts(ctx, strings.TrimSpace(researchID))
	if err != nil {
		return nil, fmt.Errorf("webresearch: list work artifacts: %w", err)
	}
	return artifactsFromWork(workArtifacts), nil
}

func (s *WorkStore) ListArtifactsForExpiredSessions(ctx context.Context, before time.Time) ([]Artifact, error) {
	workArtifacts, err := s.store.ListArtifactsForExpiredJobs(ctx, work.KindWebResearch, before)
	if err != nil {
		return nil, fmt.Errorf("webresearch: list expired work artifacts: %w", err)
	}
	return artifactsFromWork(workArtifacts), nil
}

func (s *WorkStore) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	jobs, err := s.store.ListJobs(ctx, work.JobFilter{
		Kinds:  []string{work.KindWebResearch},
		Before: &before,
	})
	if err != nil {
		return fmt.Errorf("webresearch: list expired work research jobs: %w", err)
	}
	for _, job := range jobs {
		if err := s.store.DeleteJobsByWorkerRef(ctx, job.ID); err != nil {
			return err
		}
		if err := s.store.DeleteJob(ctx, job.ID); err != nil {
			return err
		}
	}
	return nil
}

func researchJobFromWork(job work.Job) ResearchJob {
	return ResearchJob{
		ID:          job.ID,
		ResearchID:  job.WorkerRef,
		Kind:        researchKindFromWork(job.Kind),
		Status:      normalizeStatus(job.Status),
		InputJSON:   job.InputJSON,
		Error:       job.Error,
		Attempts:    job.Attempts,
		HeartbeatAt: job.HeartbeatAt,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		FinishedAt:  job.FinishedAt,
	}
}

func artifactsFromWork(workArtifacts []work.Artifact) []Artifact {
	artifacts := make([]Artifact, 0, len(workArtifacts))
	for _, artifact := range workArtifacts {
		artifacts = append(artifacts, Artifact{
			ID:         artifact.ID,
			ResearchID: artifact.JobID,
			SourceID:   artifact.SourceID,
			Kind:       artifact.Kind,
			Path:       artifact.Path,
			MIMEType:   artifact.MIMEType,
			ByteCount:  artifact.ByteCount,
			CreatedAt:  artifact.CreatedAt,
		})
	}
	return artifacts
}

func workKindForResearchJob(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "research"
	}
	return webResearchJobKindPrefix + kind
}

func researchKindFromWork(kind string) string {
	kind = strings.TrimSpace(kind)
	if strings.HasPrefix(kind, webResearchJobKindPrefix) {
		return strings.TrimPrefix(kind, webResearchJobKindPrefix)
	}
	return kind
}

func taskFromInputJSON(inputJSON string) string {
	var input struct {
		Task  string `json:"task"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
		return ""
	}
	if task := strings.Join(strings.Fields(input.Task), " "); task != "" {
		return task
	}
	return strings.Join(strings.Fields(input.Query), " ")
}

func payloadFromJob(job work.Job) researchPayload {
	var payload researchPayload
	if err := json.Unmarshal([]byte(job.ResultJSON), &payload); err != nil {
		return researchPayload{}
	}
	return payload
}

func marshalPayload(payload researchPayload) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func mapWorkErr(action string, err error) error {
	if errors.Is(err, work.ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("webresearch: %s: %w", action, err)
}
