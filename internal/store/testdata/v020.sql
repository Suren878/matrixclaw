-- A v0.2.0 database: the schema the owner's daemon had on 2026-10-18 (with the
-- orphan go-workflows and web research tables) and made-up rows. The skills
-- table predates its enabled column.
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    working_dir TEXT NOT NULL DEFAULT '',
    provider_id TEXT NOT NULL DEFAULT '',
    model_id TEXT NOT NULL DEFAULT '',
    permission_mode TEXT NOT NULL DEFAULT 'default',
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
, kind TEXT NOT NULL DEFAULT 'assistant', runtime_id TEXT NOT NULL DEFAULT 'matrixclaw', parent_session_id TEXT NOT NULL DEFAULT '', hidden INTEGER NOT NULL DEFAULT 0);
CREATE TABLE client_bindings (
    client TEXT NOT NULL,
    external_key TEXT NOT NULL,
    session_id TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (client, external_key),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    parts_json TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT '', seq INTEGER NOT NULL DEFAULT 0, origin TEXT NOT NULL DEFAULT '', compaction_json TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    user_message_id TEXT NOT NULL,
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT,
    updated_at TEXT NOT NULL, client_capabilities_json TEXT NOT NULL DEFAULT '', stop_reason TEXT NOT NULL DEFAULT '', continues_run_id TEXT NOT NULL DEFAULT '', trigger_kind TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE approvals (
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
    decided_at TEXT, reason TEXT NOT NULL DEFAULT '', suggestion_json TEXT NOT NULL DEFAULT '', task_id TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE client_deliveries (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    client TEXT NOT NULL,
    external_key TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    address_json TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT
, reply_once INTEGER NOT NULL DEFAULT 0);
CREATE TABLE automation_jobs (
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
CREATE TABLE automation_fires (
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
CREATE INDEX idx_runs_session_started_at
    ON runs(session_id, started_at);
CREATE INDEX idx_client_deliveries_status
    ON client_deliveries(client, type, status, created_at);
CREATE INDEX idx_client_deliveries_session
    ON client_deliveries(session_id, run_id, task_id);
CREATE INDEX idx_automation_jobs_due
    ON automation_jobs(status, next_due_at);
CREATE INDEX idx_automation_fires_job
    ON automation_fires(job_id, scheduled_for);
CREATE TABLE schema_migrations (version uint64,dirty bool);
CREATE UNIQUE INDEX version_unique ON schema_migrations (version);
CREATE TABLE `instances` (
  `id` TEXT NOT NULL,
  `execution_id` TEXT NOT NULL,
  `parent_instance_id` TEXT NULL,
  `parent_execution_id` TEXT NULL,
  `parent_schedule_event_id` INTEGER NULL,
  `metadata` TEXT NULL,
  `state` INTEGER NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `completed_at` DATETIME NULL,
  `locked_until` DATETIME NULL,
  `sticky_until` DATETIME NULL,
  `worker` TEXT NULL, `queue` NVARCHAR(128) DEFAULT '',
  PRIMARY KEY(`id`, `execution_id`)
);
CREATE INDEX `idx_instances_id_execution_id` ON `instances` (`id`, `execution_id`);
CREATE INDEX `idx_instances_parent_instance_id_parent_execution_id` ON `instances` (`parent_instance_id`, `parent_execution_id`);
CREATE TABLE `pending_events` (
  `id` TEXT,
  `sequence_id` INTEGER NOT NULL, -- not used but keep for now for query compat
  `instance_id` TEXT NOT NULL,
  `execution_id` TEXT NOT NULL,
  `event_type` INTEGER NOT NULL,
  `timestamp` DATETIME NOT NULL,
  `schedule_event_id` INT NOT NULL,
  `visible_at` DATETIME NULL,
  PRIMARY KEY(`id`, `instance_id`)
);
CREATE INDEX `idx_pending_events_instance_id_execution_id_visible_at_schedule_event_id` ON `pending_events` (`instance_id`, `execution_id`, `visible_at`, `schedule_event_id`);
CREATE TABLE `history` (
  `id` TEXT,
  `sequence_id` INTEGER NOT NULL,
  `instance_id` TEXT NOT NULL,
  `execution_id` TEXT NOT NULL,
  `event_type` INTEGER NOT NULL,
  `timestamp` DATETIME NOT NULL,
  `schedule_event_id` INT NOT NULL,
  `visible_at` DATETIME NULL,
  PRIMARY KEY(`id`, `instance_id`)
);
CREATE INDEX `idx_history_instance_sequence_id` ON `history` (`instance_id`, `execution_id`, `sequence_id`);
CREATE TABLE `activities` (
  `id` TEXT PRIMARY KEY,
  `instance_id` TEXT NOT NULL,
  `execution_id` TEXT NOT NULL,
  `event_type` INTEGER NOT NULL,
  `timestamp` DATETIME NOT NULL,
  `schedule_event_id` INT NOT NULL,
  `visible_at` DATETIME NULL,
  `locked_until` DATETIME NULL,
  `worker` TEXT NULL
, `queue` NVARCHAR(128) DEFAULT '');
CREATE INDEX `idx_activities_id_worker` ON `activities` (`id`, `worker`);
CREATE TABLE `attributes` (
  `id` TEXT NOT NULL,
  `instance_id` TEXT NOT NULL,
  `execution_id` TEXT NOT NULL,
  `data` BLOB NOT NULL,
  PRIMARY KEY(`id`, `instance_id`, `execution_id`)
);
CREATE INDEX `idx_instances_locked_until_completed_at_queue` ON `instances` (`completed_at`, `locked_until`, `sticky_until`, `worker`, `queue`);
CREATE INDEX `idx_activities_instance_id_execution_id_worker_queue` ON `activities` (`instance_id`, `execution_id`, `worker`, `queue`);
CREATE INDEX `idx_activities_locked_until_queue` ON `activities` (`locked_until`, `queue`);
CREATE TABLE external_agent_sessions (
    session_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    external_thread_id TEXT NOT NULL,
    external_session_id TEXT NOT NULL DEFAULT '',
    cwd TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL, approval_policy TEXT NOT NULL DEFAULT '', sandbox TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX idx_external_agent_sessions_agent
    ON external_agent_sessions(agent_id, external_thread_id);
CREATE TABLE skills (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			author TEXT NOT NULL DEFAULT '',
			authors_json TEXT NOT NULL DEFAULT '[]',
			license TEXT NOT NULL DEFAULT '',
			tags_json TEXT NOT NULL DEFAULT '[]',
			platforms_json TEXT NOT NULL DEFAULT '[]',
			category TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			provenance TEXT NOT NULL DEFAULT '',
			hash TEXT NOT NULL DEFAULT '',
			trust_state TEXT NOT NULL,
			state TEXT NOT NULL,
			pinned INTEGER NOT NULL DEFAULT 0,
			use_count INTEGER NOT NULL DEFAULT 0,
			view_count INTEGER NOT NULL DEFAULT 0,
			patch_count INTEGER NOT NULL DEFAULT 0,
			installed_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_activity_at TEXT NOT NULL DEFAULT ''
		);
CREATE VIRTUAL TABLE skill_fts USING fts5(id UNINDEXED, name, description, tags, category, body)
;
CREATE TABLE skill_sessions (
			session_id TEXT NOT NULL,
			skill_id TEXT NOT NULL,
			activated_at TEXT NOT NULL,
			PRIMARY KEY(session_id, skill_id)
		);
CREATE TABLE skill_plugin_mcp_candidates (
			id TEXT PRIMARY KEY,
			plugin_path TEXT NOT NULL,
			config_json TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);
CREATE TABLE memories (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    key TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    working_dir TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_memories_scope_workdir_updated
    ON memories(scope, working_dir, updated_at DESC);
CREATE INDEX idx_sessions_parent ON sessions(parent_session_id, hidden);
CREATE TABLE session_inputs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    target_run_id TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    parts_json TEXT NOT NULL DEFAULT '',
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL DEFAULT '',
    consumed_run_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    consumed_at TEXT, delivery_address_json TEXT NOT NULL DEFAULT '', client_capabilities_json TEXT NOT NULL DEFAULT '', reply_once INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX idx_session_inputs_session_status_created
    ON session_inputs(session_id, status, created_at);
CREATE INDEX idx_session_inputs_target_run
    ON session_inputs(target_run_id, mode, status);
CREATE TABLE web_research_sessions (
    id TEXT PRIMARY KEY,
    task TEXT NOT NULL DEFAULT '',
    query TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    answer TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    warnings_json TEXT NOT NULL DEFAULT '[]',
    next_actions_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT
);
CREATE TABLE web_research_jobs (
    id TEXT PRIMARY KEY,
    research_id TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'research',
    status TEXT NOT NULL,
    input_json TEXT NOT NULL DEFAULT '{}',
    error TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    heartbeat_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT,
    FOREIGN KEY (research_id) REFERENCES web_research_sessions(id) ON DELETE CASCADE
);
CREATE TABLE web_research_sources (
    id TEXT PRIMARY KEY,
    research_id TEXT NOT NULL,
    url TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    snippet TEXT NOT NULL DEFAULT '',
    status_code INTEGER NOT NULL DEFAULT 0,
    content_type TEXT NOT NULL DEFAULT '',
    fetched_at TEXT,
    artifact_ids_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (research_id) REFERENCES web_research_sessions(id) ON DELETE CASCADE
);
CREATE TABLE web_research_facts (
    id TEXT PRIMARY KEY,
    research_id TEXT NOT NULL,
    claim TEXT NOT NULL,
    source_ids_json TEXT NOT NULL DEFAULT '[]',
    confidence REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    FOREIGN KEY (research_id) REFERENCES web_research_sessions(id) ON DELETE CASCADE
);
CREATE TABLE web_research_artifacts (
    id TEXT PRIMARY KEY,
    research_id TEXT NOT NULL,
    source_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    path TEXT NOT NULL,
    mime_type TEXT NOT NULL DEFAULT '',
    byte_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    FOREIGN KEY (research_id) REFERENCES web_research_sessions(id) ON DELETE CASCADE
);
CREATE INDEX idx_web_research_jobs_status_updated ON web_research_jobs(status, updated_at);
CREATE INDEX idx_web_research_sources_research_url ON web_research_sources(research_id, url);
CREATE INDEX idx_web_research_facts_research ON web_research_facts(research_id, created_at);
CREATE INDEX idx_web_research_artifacts_research_source ON web_research_artifacts(research_id, source_id);
CREATE TABLE run_checkpoints (
    run_id TEXT PRIMARY KEY,
    phase TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    recovery_count INTEGER NOT NULL DEFAULT 0,
    recovery_reason TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL, engine_state TEXT NOT NULL DEFAULT '', tool_batch TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    parent_tool_call_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    command_or_goal TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL DEFAULT '',
    background INTEGER NOT NULL DEFAULT 1,
    readonly INTEGER NOT NULL DEFAULT 0,
    agent_name TEXT NOT NULL DEFAULT '',
    runtime TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    isolation TEXT NOT NULL DEFAULT '',
    pid INTEGER NOT NULL DEFAULT 0,
    pgid INTEGER NOT NULL DEFAULT 0,
    leader_start TEXT NOT NULL DEFAULT '',
    boot_id TEXT NOT NULL DEFAULT '',
    output_path TEXT NOT NULL DEFAULT '',
    exit_code INTEGER,
    output_cursor INTEGER NOT NULL DEFAULT 0,
    child_session_id TEXT NOT NULL DEFAULT '',
    child_run_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    result_message_id TEXT NOT NULL DEFAULT '',
    delivered_at TEXT,
    delivered_run_id TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE run_wakeups (
    run_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    wake_at INTEGER NOT NULL,
    task_ids_json TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);
CREATE TABLE run_steps (
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
CREATE INDEX idx_tasks_session_status
    ON tasks(session_id, status);
CREATE INDEX idx_tasks_parent_call
    ON tasks(session_id, run_id, parent_tool_call_id);
CREATE INDEX idx_tasks_child_run
    ON tasks(child_run_id);
CREATE INDEX idx_tasks_child_session
    ON tasks(child_session_id);
CREATE INDEX idx_run_wakeups_wake_at
    ON run_wakeups(wake_at);
CREATE UNIQUE INDEX idx_messages_seq ON messages(seq);
CREATE INDEX idx_messages_session_seq ON messages(session_id, seq);
CREATE INDEX idx_messages_boundaries ON messages(session_id, seq) WHERE compaction_json <> '';
CREATE TABLE session_budgets (
    session_id TEXT PRIMARY KEY,
    steps INTEGER,
    active_seconds INTEGER,
    tokens INTEGER,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE session_engine_state (
    session_id TEXT PRIMARY KEY,
    engine_state TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE session_todos (
    session_id TEXT PRIMARY KEY,
    items_json TEXT NOT NULL,
    chain_run_id TEXT NOT NULL DEFAULT '',
    updated_run_id TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE TABLE permission_rules (
    id TEXT PRIMARY KEY,
    tool TEXT NOT NULL,
    pattern TEXT NOT NULL DEFAULT '',
    effect TEXT NOT NULL,
    scope TEXT NOT NULL,
    session_id TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX idx_permission_rules_session ON permission_rules(session_id);
CREATE VIRTUAL TABLE message_fts USING fts5(role, content, provider, model, tokenize = 'unicode61')
;
CREATE TRIGGER messages_search_delete AFTER DELETE ON messages BEGIN
    DELETE FROM message_fts WHERE rowid = old.seq;
END;
CREATE INDEX idx_messages_session_run ON messages(session_id, run_id, seq);
CREATE INDEX idx_approvals_session_run_state ON approvals(session_id, run_id, state);
CREATE INDEX idx_approvals_task ON approvals(task_id) WHERE task_id <> '';

-- Made-up rows.
INSERT INTO sessions(id, title, status, created_at, updated_at, kind, runtime_id, permission_mode) VALUES
    ('s1', 'Chat', 'active', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', 'assistant', 'matrixclaw', 'default'),
    ('s2', 'Codex', 'active', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', 'external_agent', 'external_agent', 'full_auto');
INSERT INTO client_bindings(client, external_key, session_id, updated_at) VALUES ('telegram', 'chat:1', 's1', '2026-10-01T10:00:00Z');
INSERT INTO messages(id, session_id, run_id, role, content, parts_json, created_at, updated_at, seq) VALUES
    ('m1', 's1', 'r1', 'user', 'Check the logs', '', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', 1),
    ('m2', 's1', 'r1', 'assistant', '', '[{"kind":"tool_call","tool_call":{"id":"c1","name":"bash","input":"{}","finished":true}},{"kind":"tool_call","tool_call":{"id":"c2","name":"bash","input":"{}","finished":true}}]', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', 2),
    ('m3', 's1', 'r1', 'tool', 'ping: socket: Operation not permitted', '[{"kind":"tool_result","tool_result":{"tool_call_id":"c1","name":"bash","content":"ping: socket: Operation not permitted","is_error":true}}]', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', 3),
    ('m4', 's1', 'r1', 'tool', 'Tool completed', json_array(json_object('kind', 'tool_result', 'tool_result', json_object('tool_call_id', 'c2', 'name', 'bash', 'content', 'HEAD ' || replace(hex(zeroblob(20000)), '00', 'log line ') || 'TAIL'))), '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', 4);
INSERT INTO message_fts(rowid, role, content, provider, model) SELECT seq, role, content, provider, model FROM messages;
INSERT INTO runs(id, session_id, user_message_id, client, external_key, status, started_at, finished_at, updated_at, client_capabilities_json) VALUES
    ('r1', 's1', 'm1', 'telegram', 'chat:1', 'completed', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', '{"supports_voice_delivery":true,"supports_document_delivery":true}'),
    ('r2', 's1', 'm1', 'terminal', 'local', 'completed', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', '');
INSERT INTO run_steps(run_id, step, model, provider, prompt_tokens, output_tokens, created_at) VALUES ('r1', 1, 'gpt', 'openai-compatible', 100, 10, '2026-10-01T10:00:00Z');
INSERT INTO run_checkpoints(run_id, phase, tool_call_id, tool_name, recovery_count, recovery_reason, updated_at) VALUES ('r1', 'tool', 'c1', 'bash', 1, 'restart', '2026-10-01T10:00:00Z');
INSERT INTO approvals(id, session_id, run_id, tool_call_ref, tool_name, action, state, requested_at, decided_at) VALUES ('a1', 's1', 'r1', 'c1', 'bash', 'execute', 'approved', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
INSERT INTO tasks(id, session_id, run_id, kind, status, command_or_goal, result_message_id, started_at, updated_at) VALUES ('t1', 's1', 'r1', 'shell', 'completed', 'make test', 'm3', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
INSERT INTO session_inputs(id, session_id, mode, status, text, client, client_capabilities_json, created_at, updated_at) VALUES
    ('i1', 's1', 'queue', 'consumed', 'Later', 'telegram', '{"supports_voice_delivery":true}', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
INSERT INTO client_deliveries(id, type, client, external_key, session_id, run_id, status, created_at, updated_at) VALUES
    ('d1', 'run', 'telegram', 'chat:1', 's1', 'r1', 'sent', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z'),
    ('d2', 'run', 'terminal:local', 'local', 's1', 'r2', 'pending', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
INSERT INTO external_agent_sessions(session_id, agent_id, external_thread_id, created_at, updated_at, approval_policy, sandbox) VALUES ('s2', 'codex-app', 'thread-1', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z', 'never', 'danger-full-access');
INSERT INTO memories(id, scope, content, created_at, updated_at) VALUES ('mem1', 'global', 'Prefers short replies', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
INSERT INTO permission_rules(id, tool, pattern, effect, scope, created_at) VALUES ('rule1', 'bash', 'git status', 'allow', 'global', '2026-10-01T10:00:00Z');
INSERT INTO automation_jobs(id, kind, status, session_id, schedule_mode, created_at, updated_at) VALUES ('job1', 'prompt', 'active', 's1', 'once', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
INSERT INTO skills(id, name, description, path, trust_state, state, installed_at, updated_at) VALUES ('review', 'Review', 'Review a diff', '/skills/review', 'trusted', 'active', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
INSERT INTO skill_fts(id, name, description, tags, category, body) VALUES ('review', 'Review', 'Review a diff', '', '', 'Read the diff twice.');
INSERT INTO schema_migrations(version, dirty) VALUES (3, 0);
INSERT INTO history(id, sequence_id, instance_id, execution_id, event_type, timestamp, schedule_event_id) VALUES ('h1', 1, 'run_r1_1', 'e1', 6, '2026-10-01T10:00:00Z', 0);
INSERT INTO web_research_sessions(id, status, created_at, updated_at) VALUES ('res1', 'completed', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
INSERT INTO web_research_jobs(id, research_id, status, created_at, updated_at) VALUES ('job-res1', 'res1', 'completed', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z');
