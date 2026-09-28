package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// migrateSubagentTasks copies the rows of subagent_tasks into tasks and drops
// it. A finished task never queued for its parent counts as delivered; columns
// that older databases lack take their defaults.
func migrateSubagentTasks(db *sql.DB) error {
	columns, err := tableColumns(db, "subagent_tasks")
	if err != nil || len(columns) == 0 {
		return err
	}
	column := func(name, fallback string) string {
		if columns[name] {
			return name
		}
		return fallback
	}
	queued := column("completion_queued_at", "NULL")
	copyRows := `
INSERT OR IGNORE INTO tasks(id, session_id, run_id, parent_tool_call_id, kind, status, command_or_goal,
    description, background, agent_name, runtime, isolation, child_session_id, child_run_id, summary, error,
    result_message_id, delivered_at, delivered_run_id, started_at, updated_at, finished_at)
SELECT id, parent_session_id, parent_run_id, parent_tool_call_id, 'subagent', status, goal,
    ` + column("display_name", "''") + `,
    CASE WHEN ` + column("mode", "'blocking'") + ` = 'async' THEN 1 ELSE 0 END,
    ` + column("agent_name", "''") + `, runtime, ` + column("isolation", "'shared'") + `,
    child_session_id, child_run_id, summary, error, ` + column("result_message_id", "''") + `,
    COALESCE(` + column("completion_delivered_at", "NULL") + `, CASE WHEN ` + queued + ` IS NULL THEN finished_at END),
    ` + column("completion_auto_resume_run_id", "''") + `, created_at, updated_at, finished_at
FROM subagent_tasks
WHERE parent_session_id IN (SELECT id FROM sessions)`
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin subagent_tasks migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(copyRows); err != nil {
		return fmt.Errorf("store: copy subagent_tasks into tasks: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE subagent_tasks`); err != nil {
		return fmt.Errorf("store: drop subagent_tasks: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit subagent_tasks migration: %w", err)
	}
	return nil
}

// tableColumns names the columns of table; none when it does not exist.
func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("store: inspect %s schema: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store: scan %s schema: %w", table, err)
		}
		columns[strings.ToLower(name)] = true
	}
	return columns, rows.Err()
}
