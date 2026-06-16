package work

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("work: not found")

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("work: sqlite path is required")
	}
	cleanPath := filepath.Clean(path)
	if dir := filepath.Dir(cleanPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("work: create sqlite dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", cleanPath)
	if err != nil {
		return nil, fmt.Errorf("work: open sqlite: %w", err)
	}
	// The work store follows the daemon's personal-process SQLite contract:
	// serialize access instead of pretending this is a multi-user database.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLiteStore{db: db}
	if err := store.bootstrap(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) bootstrap() error {
	for _, stmt := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("work: bootstrap pragma: %w", err)
		}
	}
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS work_jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    task TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    result_json TEXT NOT NULL DEFAULT '{}',
    worker_ref TEXT NOT NULL DEFAULT '',
    token_usage_json TEXT NOT NULL DEFAULT '{}',
    input_json TEXT NOT NULL DEFAULT '{}',
    error TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    heartbeat_at TEXT,
    started_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT
);
CREATE TABLE IF NOT EXISTS work_artifacts (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    source_id TEXT NOT NULL DEFAULT '',
    source_url TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    path TEXT NOT NULL,
    mime_type TEXT NOT NULL DEFAULT '',
    byte_count INTEGER NOT NULL DEFAULT 0,
    summary TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    FOREIGN KEY (job_id) REFERENCES work_jobs(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS work_facts (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    claim TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 0,
    source_ids_json TEXT NOT NULL DEFAULT '[]',
    artifact_ids_json TEXT NOT NULL DEFAULT '[]',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    FOREIGN KEY (job_id) REFERENCES work_jobs(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_work_jobs_kind_status_updated ON work_jobs(kind, status, updated_at);
CREATE INDEX IF NOT EXISTS idx_work_jobs_worker_ref ON work_jobs(worker_ref, created_at);
CREATE INDEX IF NOT EXISTS idx_work_artifacts_job_source ON work_artifacts(job_id, source_id);
CREATE INDEX IF NOT EXISTS idx_work_facts_job_created ON work_facts(job_id, created_at);
`); err != nil {
		return fmt.Errorf("work: apply schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateJob(ctx context.Context, job Job) error {
	job = normalizeJob(job)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO work_jobs(id, kind, status, task, summary, result_json, worker_ref, token_usage_json, input_json, error, attempts, heartbeat_at, started_at, created_at, updated_at, finished_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Kind, job.Status, job.Task, job.Summary, defaultJSON(job.ResultJSON), job.WorkerRef,
		defaultJSON(job.TokenUsageJSON), defaultJSON(job.InputJSON), job.Error, job.Attempts,
		nullableTime(job.HeartbeatAt), nullableTime(job.StartedAt), formatTime(job.CreatedAt), formatTime(job.UpdatedAt), nullableTime(job.FinishedAt))
	if err != nil {
		return fmt.Errorf("work: create job: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateJob(ctx context.Context, job Job) error {
	job = normalizeJob(job)
	result, err := s.db.ExecContext(ctx, `
UPDATE work_jobs
SET kind = ?, status = ?, task = ?, summary = ?, result_json = ?, worker_ref = ?, token_usage_json = ?,
    input_json = ?, error = ?, attempts = ?, heartbeat_at = ?, started_at = ?, updated_at = ?, finished_at = ?
WHERE id = ?`,
		job.Kind, job.Status, job.Task, job.Summary, defaultJSON(job.ResultJSON), job.WorkerRef,
		defaultJSON(job.TokenUsageJSON), defaultJSON(job.InputJSON), job.Error, job.Attempts,
		nullableTime(job.HeartbeatAt), nullableTime(job.StartedAt), formatTime(job.UpdatedAt), nullableTime(job.FinishedAt), job.ID)
	if err != nil {
		return fmt.Errorf("work: update job: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("work: update job rows: %w", err)
	} else if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) GetJob(ctx context.Context, id string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, kind, status, task, summary, result_json, worker_ref, token_usage_json, input_json, error, attempts, heartbeat_at, started_at, created_at, updated_at, finished_at
FROM work_jobs WHERE id = ?`, strings.TrimSpace(id))
	return scanJob(row)
}

func (s *SQLiteStore) ListJobs(ctx context.Context, filter JobFilter) ([]Job, error) {
	query := `SELECT id, kind, status, task, summary, result_json, worker_ref, token_usage_json, input_json, error, attempts, heartbeat_at, started_at, created_at, updated_at, finished_at FROM work_jobs`
	var where []string
	var args []any
	if len(filter.Kinds) > 0 {
		placeholders := make([]string, 0, len(filter.Kinds))
		for _, kind := range filter.Kinds {
			placeholders = append(placeholders, "?")
			args = append(args, strings.TrimSpace(kind))
		}
		where = append(where, "kind IN ("+strings.Join(placeholders, ", ")+")")
	}
	if strings.TrimSpace(filter.KindPrefix) != "" {
		where = append(where, "kind LIKE ?")
		args = append(args, strings.TrimSpace(filter.KindPrefix)+"%")
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			placeholders = append(placeholders, "?")
			args = append(args, normalizeStatus(status))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ", ")+")")
	}
	if strings.TrimSpace(filter.WorkerRef) != "" {
		where = append(where, "worker_ref = ?")
		args = append(args, strings.TrimSpace(filter.WorkerRef))
	}
	if filter.Before != nil {
		where = append(where, "COALESCE(finished_at, updated_at) < ?")
		args = append(args, formatTime(*filter.Before))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at ASC, id ASC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("work: list jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanJobs(rows)
}

func (s *SQLiteStore) DeleteJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM work_jobs WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("work: delete job: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteJobsByWorkerRef(ctx context.Context, workerRef string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM work_jobs WHERE worker_ref = ?`, strings.TrimSpace(workerRef))
	if err != nil {
		return fmt.Errorf("work: delete jobs by worker ref: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateArtifact(ctx context.Context, artifact Artifact) error {
	artifact.MetadataJSON = defaultJSON(artifact.MetadataJSON)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO work_artifacts(id, job_id, source_id, source_url, kind, path, mime_type, byte_count, summary, metadata_json, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		artifact.ID, artifact.JobID, artifact.SourceID, artifact.SourceURL, artifact.Kind, artifact.Path,
		artifact.MIMEType, artifact.ByteCount, artifact.Summary, artifact.MetadataJSON, formatTime(artifact.CreatedAt))
	if err != nil {
		return fmt.Errorf("work: create artifact: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListArtifacts(ctx context.Context, jobID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, source_id, source_url, kind, path, mime_type, byte_count, summary, metadata_json, created_at
FROM work_artifacts WHERE job_id = ?
ORDER BY created_at ASC, id ASC`, strings.TrimSpace(jobID))
	if err != nil {
		return nil, fmt.Errorf("work: list artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanArtifacts(rows)
}

func (s *SQLiteStore) ListArtifactsForExpiredJobs(ctx context.Context, kind string, before time.Time) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT a.id, a.job_id, a.source_id, a.source_url, a.kind, a.path, a.mime_type, a.byte_count, a.summary, a.metadata_json, a.created_at
FROM work_artifacts a
JOIN work_jobs j ON j.id = a.job_id
WHERE j.kind = ? AND COALESCE(j.finished_at, j.updated_at) < ?
ORDER BY a.created_at ASC, a.id ASC`, strings.TrimSpace(kind), formatTime(before))
	if err != nil {
		return nil, fmt.Errorf("work: list expired artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanArtifacts(rows)
}

func (s *SQLiteStore) ReplaceFacts(ctx context.Context, jobID string, facts []Fact) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("work: begin replace facts: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM work_facts WHERE job_id = ?`, strings.TrimSpace(jobID)); err != nil {
		return fmt.Errorf("work: delete facts: %w", err)
	}
	for _, fact := range facts {
		_, err := tx.ExecContext(ctx, `
INSERT INTO work_facts(id, job_id, claim, confidence, source_ids_json, artifact_ids_json, metadata_json, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			fact.ID, jobID, fact.Claim, fact.Confidence, jsonList(fact.SourceIDs), jsonList(fact.ArtifactIDs),
			defaultJSON(fact.MetadataJSON), formatTime(fact.CreatedAt))
		if err != nil {
			return fmt.Errorf("work: insert fact: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("work: commit replace facts: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListFacts(ctx context.Context, jobID string) ([]Fact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, claim, confidence, source_ids_json, artifact_ids_json, metadata_json, created_at
FROM work_facts WHERE job_id = ?
ORDER BY created_at ASC, id ASC`, strings.TrimSpace(jobID))
	if err != nil {
		return nil, fmt.Errorf("work: list facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var facts []Fact
	for rows.Next() {
		var fact Fact
		var sourceIDsJSON, artifactIDsJSON, created string
		if err := rows.Scan(&fact.ID, &fact.JobID, &fact.Claim, &fact.Confidence, &sourceIDsJSON, &artifactIDsJSON, &fact.MetadataJSON, &created); err != nil {
			return nil, fmt.Errorf("work: scan fact: %w", err)
		}
		fact.SourceIDs = parseStringList(sourceIDsJSON)
		fact.ArtifactIDs = parseStringList(artifactIDsJSON)
		fact.CreatedAt = parseTime(created)
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("work: iterate facts: %w", err)
	}
	return facts, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(row scanner) (Job, error) {
	var job Job
	var heartbeat, started, finished sql.NullString
	var created, updated string
	if err := row.Scan(&job.ID, &job.Kind, &job.Status, &job.Task, &job.Summary, &job.ResultJSON, &job.WorkerRef,
		&job.TokenUsageJSON, &job.InputJSON, &job.Error, &job.Attempts, &heartbeat, &started, &created, &updated, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, fmt.Errorf("work: scan job: %w", err)
	}
	if heartbeat.Valid {
		t := parseTime(heartbeat.String)
		job.HeartbeatAt = &t
	}
	if started.Valid {
		t := parseTime(started.String)
		job.StartedAt = &t
	}
	job.CreatedAt = parseTime(created)
	job.UpdatedAt = parseTime(updated)
	if finished.Valid {
		t := parseTime(finished.String)
		job.FinishedAt = &t
	}
	return job, nil
}

func scanJobs(rows *sql.Rows) ([]Job, error) {
	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("work: iterate jobs: %w", err)
	}
	return jobs, nil
}

func scanArtifacts(rows *sql.Rows) ([]Artifact, error) {
	var artifacts []Artifact
	for rows.Next() {
		var artifact Artifact
		var created string
		if err := rows.Scan(&artifact.ID, &artifact.JobID, &artifact.SourceID, &artifact.SourceURL, &artifact.Kind, &artifact.Path,
			&artifact.MIMEType, &artifact.ByteCount, &artifact.Summary, &artifact.MetadataJSON, &created); err != nil {
			return nil, fmt.Errorf("work: scan artifact: %w", err)
		}
		artifact.CreatedAt = parseTime(created)
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("work: iterate artifacts: %w", err)
	}
	return artifacts, nil
}

func normalizeJob(job Job) Job {
	job.Kind = strings.TrimSpace(job.Kind)
	if job.Kind == "" {
		job.Kind = "generic"
	}
	job.Status = normalizeStatus(job.Status)
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = job.CreatedAt
	}
	job.ResultJSON = defaultJSON(job.ResultJSON)
	job.TokenUsageJSON = defaultJSON(job.TokenUsageJSON)
	job.InputJSON = defaultJSON(job.InputJSON)
	return job
}

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusRunning:
		return StatusRunning
	case StatusCompleted:
		return StatusCompleted
	case StatusFailed:
		return StatusFailed
	case StatusCanceled:
		return StatusCanceled
	default:
		return StatusPending
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return formatTime(*value)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	return time.Time{}
}

func jsonList(values []string) string {
	if values == nil {
		return "[]"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseStringList(value string) []string {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil
	}
	return out
}

func defaultJSON(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "{}"
	}
	return value
}
