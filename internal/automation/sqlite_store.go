package automation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

var _ Store = (*SQLiteStore)(nil)

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("automation store: sqlite path is required")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("automation store: open sqlite: %w", err)
	}
	// Automation runs inside one daemon process; keep SQLite access serialized
	// so scheduled job state transitions remain simple and deterministic.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, stmt := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("automation store: bootstrap pragma %q: %w", stmt, err)
		}
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) CreateAutomationJob(ctx context.Context, job Job) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO automation_jobs(id, kind, status, session_id, client, external_key, title, timezone, schedule_mode, run_at, interval_seconds, cron_expr, next_due_at, last_scheduled_for, prompt, created_at, updated_at, deleted_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID,
		string(job.Kind),
		string(job.Status),
		job.SessionID,
		job.Client,
		job.ExternalKey,
		job.Title,
		job.Timezone,
		string(job.ScheduleMode),
		sqliteNullableTime(job.RunAt),
		job.IntervalSeconds,
		job.CronExpr,
		sqliteNullableTime(job.NextDueAt),
		sqliteNullableTime(job.LastScheduledFor),
		job.Prompt,
		sqliteFormatTime(job.CreatedAt),
		sqliteFormatTime(job.UpdatedAt),
		sqliteNullableTime(job.DeletedAt),
	)
	if err != nil {
		return fmt.Errorf("automation store: create job: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetAutomationJob(ctx context.Context, jobID string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, kind, status, session_id, client, external_key, title, timezone, schedule_mode, run_at, interval_seconds, cron_expr, next_due_at, last_scheduled_for, prompt, created_at, updated_at, deleted_at
FROM automation_jobs
WHERE id = ?`, jobID)
	job, err := scanSQLiteAutomationJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, core.ErrNotFound
		}
		return Job{}, err
	}
	return job, nil
}

func (s *SQLiteStore) ListAutomationJobs(ctx context.Context, filter JobFilter) ([]Job, error) {
	query := `
SELECT id, kind, status, session_id, client, external_key, title, timezone, schedule_mode, run_at, interval_seconds, cron_expr, next_due_at, last_scheduled_for, prompt, created_at, updated_at, deleted_at
FROM automation_jobs`
	args := []any{}
	clauses := []string{}
	if !filter.IncludeDeleted {
		clauses = append(clauses, "status != ?")
		args = append(args, string(JobStatusDeleted))
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(filter.Status))
	}
	if strings.TrimSpace(filter.SessionID) != "" {
		clauses = append(clauses, "session_id = ?")
		args = append(args, strings.TrimSpace(filter.SessionID))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY COALESCE(next_due_at, updated_at) ASC, created_at ASC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("automation store: list jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	jobs := []Job{}
	for rows.Next() {
		job, err := scanSQLiteAutomationJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("automation store: iterate jobs: %w", err)
	}
	return jobs, nil
}

func (s *SQLiteStore) UpdateAutomationJob(ctx context.Context, job Job) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE automation_jobs
SET kind = ?, status = ?, session_id = ?, client = ?, external_key = ?, title = ?, timezone = ?, schedule_mode = ?, run_at = ?, interval_seconds = ?, cron_expr = ?, next_due_at = ?, last_scheduled_for = ?, prompt = ?, updated_at = ?, deleted_at = ?
WHERE id = ?`,
		string(job.Kind),
		string(job.Status),
		job.SessionID,
		job.Client,
		job.ExternalKey,
		job.Title,
		job.Timezone,
		string(job.ScheduleMode),
		sqliteNullableTime(job.RunAt),
		job.IntervalSeconds,
		job.CronExpr,
		sqliteNullableTime(job.NextDueAt),
		sqliteNullableTime(job.LastScheduledFor),
		job.Prompt,
		sqliteFormatTime(job.UpdatedAt),
		sqliteNullableTime(job.DeletedAt),
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("automation store: update job: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("automation store: update job rows: %w", err)
	} else if rows == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) CreateAutomationFire(ctx context.Context, fire Fire) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO automation_fires(id, job_id, scheduled_for, status, result_state, run_id, error, created_at, updated_at, finished_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fire.ID,
		fire.JobID,
		sqliteFormatTime(fire.ScheduledFor),
		string(fire.Status),
		fire.ResultState,
		fire.RunID,
		fire.Error,
		sqliteFormatTime(fire.CreatedAt),
		sqliteFormatTime(fire.UpdatedAt),
		sqliteNullableTime(fire.FinishedAt),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrDuplicateFire
		}
		return fmt.Errorf("automation store: create fire: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetAutomationFireBySchedule(ctx context.Context, jobID string, scheduledFor time.Time) (Fire, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, job_id, scheduled_for, status, result_state, run_id, error, created_at, updated_at, finished_at
FROM automation_fires
WHERE job_id = ? AND scheduled_for = ?`, jobID, sqliteFormatTime(scheduledFor))
	fire, err := scanSQLiteAutomationFire(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Fire{}, core.ErrNotFound
		}
		return Fire{}, err
	}
	return fire, nil
}

func (s *SQLiteStore) UpdateAutomationFire(ctx context.Context, fire Fire) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE automation_fires
SET status = ?, result_state = ?, run_id = ?, error = ?, updated_at = ?, finished_at = ?
WHERE id = ?`,
		string(fire.Status),
		fire.ResultState,
		fire.RunID,
		fire.Error,
		sqliteFormatTime(fire.UpdatedAt),
		sqliteNullableTime(fire.FinishedAt),
		fire.ID,
	)
	if err != nil {
		return fmt.Errorf("automation store: update fire: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("automation store: update fire rows: %w", err)
	} else if rows == 0 {
		return core.ErrNotFound
	}
	return nil
}

type sqliteAutomationJobScanner interface {
	Scan(dest ...any) error
}

type sqliteAutomationFireScanner interface {
	Scan(dest ...any) error
}

func scanSQLiteAutomationJob(scanner sqliteAutomationJobScanner) (Job, error) {
	var job Job
	var kind string
	var status string
	var scheduleMode string
	var runAt sql.NullString
	var nextDueAt sql.NullString
	var lastScheduledFor sql.NullString
	var createdAt string
	var updatedAt string
	var deletedAt sql.NullString
	if err := scanner.Scan(
		&job.ID,
		&kind,
		&status,
		&job.SessionID,
		&job.Client,
		&job.ExternalKey,
		&job.Title,
		&job.Timezone,
		&scheduleMode,
		&runAt,
		&job.IntervalSeconds,
		&job.CronExpr,
		&nextDueAt,
		&lastScheduledFor,
		&job.Prompt,
		&createdAt,
		&updatedAt,
		&deletedAt,
	); err != nil {
		return Job{}, fmt.Errorf("automation store: scan job: %w", err)
	}
	job.Kind = JobKind(kind)
	job.Status = JobStatus(status)
	job.ScheduleMode = ScheduleMode(scheduleMode)
	if runAt.Valid {
		parsed := sqliteParseTime(runAt.String)
		job.RunAt = &parsed
	}
	if nextDueAt.Valid {
		parsed := sqliteParseTime(nextDueAt.String)
		job.NextDueAt = &parsed
	}
	if lastScheduledFor.Valid {
		parsed := sqliteParseTime(lastScheduledFor.String)
		job.LastScheduledFor = &parsed
	}
	job.CreatedAt = sqliteParseTime(createdAt)
	job.UpdatedAt = sqliteParseTime(updatedAt)
	if deletedAt.Valid {
		parsed := sqliteParseTime(deletedAt.String)
		job.DeletedAt = &parsed
	}
	return job, nil
}

func scanSQLiteAutomationFire(scanner sqliteAutomationFireScanner) (Fire, error) {
	var fire Fire
	var status string
	var scheduledFor string
	var createdAt string
	var updatedAt string
	var finishedAt sql.NullString
	if err := scanner.Scan(
		&fire.ID,
		&fire.JobID,
		&scheduledFor,
		&status,
		&fire.ResultState,
		&fire.RunID,
		&fire.Error,
		&createdAt,
		&updatedAt,
		&finishedAt,
	); err != nil {
		return Fire{}, fmt.Errorf("automation store: scan fire: %w", err)
	}
	fire.Status = FireStatus(status)
	fire.ScheduledFor = sqliteParseTime(scheduledFor)
	fire.CreatedAt = sqliteParseTime(createdAt)
	fire.UpdatedAt = sqliteParseTime(updatedAt)
	if finishedAt.Valid {
		parsed := sqliteParseTime(finishedAt.String)
		fire.FinishedAt = &parsed
	}
	return fire, nil
}

func sqliteFormatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func sqliteNullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func sqliteParseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		if parsed, err = time.Parse(time.RFC3339, value); err == nil {
			return parsed.UTC()
		}
		if unix, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil {
			return time.Unix(0, unix).UTC()
		}
		return time.Time{}
	}
	return parsed
}
