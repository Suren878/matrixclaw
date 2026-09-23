package store

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// migrateMessageSeq numbers messages database-wide; rows written before seq
// existed are numbered by parsed created_at, then rowid.
func migrateMessageSeq(db *sql.DB) error {
	if err := ensureColumn(db, "messages", "seq", `ALTER TABLE messages ADD COLUMN seq INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := backfillMessageSeq(db); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_messages_session_created_at`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_seq ON messages(seq)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session_seq ON messages(session_id, seq)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("store: index message seq: %w", err)
		}
	}
	return nil
}

func backfillMessageSeq(db *sql.DB) error {
	type unsequenced struct {
		rowid     int64
		createdAt time.Time
	}
	rows, err := db.Query(`SELECT rowid, created_at FROM messages WHERE seq = 0 ORDER BY rowid`)
	if err != nil {
		return fmt.Errorf("store: list unsequenced messages: %w", err)
	}
	var pending []unsequenced
	for rows.Next() {
		var row unsequenced
		var createdAt string
		if err := rows.Scan(&row.rowid, &createdAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: scan unsequenced message: %w", err)
		}
		row.createdAt = mustParseTime(createdAt)
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate unsequenced messages: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].createdAt.Before(pending[j].createdAt) })

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin message seq backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var next int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM messages`).Scan(&next); err != nil {
		return fmt.Errorf("store: read max message seq: %w", err)
	}
	for _, row := range pending {
		next++
		if _, err := tx.Exec(`UPDATE messages SET seq = ? WHERE rowid = ?`, next, row.rowid); err != nil {
			return fmt.Errorf("store: backfill message seq: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit message seq backfill: %w", err)
	}
	return nil
}
