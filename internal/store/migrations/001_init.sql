CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    working_dir TEXT NOT NULL DEFAULT '',
    provider_id TEXT NOT NULL DEFAULT '',
    model_id TEXT NOT NULL DEFAULT '',
    permission_mode TEXT NOT NULL DEFAULT 'default',
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS client_bindings (
    client TEXT NOT NULL,
    external_key TEXT NOT NULL,
    session_id TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (client, external_key),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    parts_json TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    user_message_id TEXT NOT NULL,
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS approvals (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    tool_call_ref TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    params_json TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    requested_at TEXT NOT NULL,
    decided_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS file_snapshots (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    path TEXT NOT NULL,
    content TEXT NOT NULL,
    version INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(session_id, path, version),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS client_deliveries (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    client TEXT NOT NULL,
    external_key TEXT NOT NULL,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    summary TEXT NOT NULL,
    address_json TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT
);

CREATE TABLE IF NOT EXISTS automation_jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    session_id TEXT NOT NULL,
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT 'UTC',
    schedule_mode TEXT NOT NULL,
    run_at TEXT,
    interval_seconds INTEGER NOT NULL DEFAULT 0,
    cron_expr TEXT NOT NULL DEFAULT '',
    next_due_at TEXT,
    last_scheduled_for TEXT,
    prompt TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS automation_fires (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    scheduled_for TEXT NOT NULL,
    status TEXT NOT NULL,
    result_state TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT,
    UNIQUE(job_id, scheduled_for),
    FOREIGN KEY (job_id) REFERENCES automation_jobs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_messages_session_created_at
    ON messages(session_id, created_at);

CREATE INDEX IF NOT EXISTS idx_runs_session_started_at
    ON runs(session_id, started_at);

CREATE INDEX IF NOT EXISTS idx_file_snapshots_session_path_version
    ON file_snapshots(session_id, path, version DESC);

CREATE INDEX IF NOT EXISTS idx_client_deliveries_status
    ON client_deliveries(client, type, status, created_at);

CREATE INDEX IF NOT EXISTS idx_client_deliveries_session
    ON client_deliveries(session_id, run_id, task_id);

CREATE INDEX IF NOT EXISTS idx_automation_jobs_due
    ON automation_jobs(status, next_due_at);

CREATE INDEX IF NOT EXISTS idx_automation_fires_job
    ON automation_fires(job_id, scheduled_for);
