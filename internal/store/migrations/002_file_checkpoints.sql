CREATE TABLE file_checkpoints (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    seq INTEGER NOT NULL DEFAULT 0,
    tool_name TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL,
    tree TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_file_checkpoints_session ON file_checkpoints(session_id, created_at);
CREATE INDEX idx_file_checkpoints_working_dir ON file_checkpoints(working_dir, created_at);
