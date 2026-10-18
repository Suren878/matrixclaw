CREATE TABLE session_goals (
    session_id TEXT PRIMARY KEY,
    objective TEXT NOT NULL,
    check_command TEXT NOT NULL DEFAULT '',
    max_runs INTEGER NOT NULL,
    token_budget INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    runs_used INTEGER NOT NULL DEFAULT 0,
    tokens_used INTEGER NOT NULL DEFAULT 0,
    last_run_id TEXT NOT NULL DEFAULT '',
    last_stop TEXT NOT NULL DEFAULT '',
    last_check TEXT NOT NULL DEFAULT '',
    check_failure TEXT NOT NULL DEFAULT '',
    check_failures INTEGER NOT NULL DEFAULT 0,
    last_blocker TEXT NOT NULL DEFAULT '',
    blocker_repeats INTEGER NOT NULL DEFAULT 0,
    blocker_run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_session_goals_status ON session_goals(status);
