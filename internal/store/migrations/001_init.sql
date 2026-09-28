CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'assistant',
    runtime_id TEXT NOT NULL DEFAULT 'matrixclaw',
    parent_session_id TEXT NOT NULL DEFAULT '',
    hidden INTEGER NOT NULL DEFAULT 0,
    working_dir TEXT NOT NULL DEFAULT '',
    provider_id TEXT NOT NULL DEFAULT '',
    model_id TEXT NOT NULL DEFAULT '',
    permission_mode TEXT NOT NULL DEFAULT 'default',
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS subagent_tasks (
    id TEXT PRIMARY KEY,
    agent_name TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'blocking',
    isolation TEXT NOT NULL DEFAULT 'shared',
    parent_session_id TEXT NOT NULL,
    parent_run_id TEXT NOT NULL DEFAULT '',
    parent_tool_call_id TEXT NOT NULL DEFAULT '',
    child_session_id TEXT NOT NULL DEFAULT '',
    child_run_id TEXT NOT NULL DEFAULT '',
    runtime TEXT NOT NULL,
    goal TEXT NOT NULL,
    status TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    result_message_id TEXT NOT NULL DEFAULT '',
    completion_queued_at TEXT,
    completion_delivered_at TEXT,
    completion_auto_resume_run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT,
    FOREIGN KEY (parent_session_id) REFERENCES sessions(id) ON DELETE CASCADE
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
    seq INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    key TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    working_dir TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    user_message_id TEXT NOT NULL,
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    client_capabilities_json TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS run_checkpoints (
    run_id TEXT PRIMARY KEY,
    phase TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    recovery_count INTEGER NOT NULL DEFAULT 0,
    recovery_reason TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS session_inputs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    target_run_id TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    parts_json TEXT NOT NULL DEFAULT '',
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    client_capabilities_json TEXT NOT NULL DEFAULT '',
    delivery_address_json TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL DEFAULT '',
    consumed_run_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    consumed_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS run_steps (
    run_id TEXT NOT NULL,
    step INTEGER NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    stop_reason TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    tool_calls INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    PRIMARY KEY (run_id, step),
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
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
    payload_json TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS external_agent_sessions (
    session_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    external_thread_id TEXT NOT NULL,
    external_session_id TEXT NOT NULL DEFAULT '',
    cwd TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    approval_policy TEXT NOT NULL DEFAULT '',
    sandbox TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_subagent_tasks_parent
    ON subagent_tasks(parent_session_id, parent_run_id, parent_tool_call_id);

CREATE INDEX IF NOT EXISTS idx_subagent_tasks_child_run
    ON subagent_tasks(child_run_id);

CREATE INDEX IF NOT EXISTS idx_runs_session_started_at
    ON runs(session_id, started_at);

CREATE INDEX IF NOT EXISTS idx_session_inputs_session_status_created
    ON session_inputs(session_id, status, created_at);

CREATE INDEX IF NOT EXISTS idx_session_inputs_target_run
    ON session_inputs(target_run_id, mode, status);

CREATE INDEX IF NOT EXISTS idx_memories_scope_workdir_updated
    ON memories(scope, working_dir, updated_at DESC);

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

CREATE INDEX IF NOT EXISTS idx_external_agent_sessions_agent
    ON external_agent_sessions(agent_id, external_thread_id);
