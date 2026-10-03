package store

import (
	"context"
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
		`CREATE INDEX IF NOT EXISTS idx_messages_session_run ON messages(session_id, run_id, seq)`,
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
	defer func() { _ = rows.Close() }()
	var pending []unsequenced
	for rows.Next() {
		var row unsequenced
		var createdAt string
		if err := rows.Scan(&row.rowid, &createdAt); err != nil {
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

// migrateMessageSearch keys message_fts rows by messages.seq, rebuilding an
// index still keyed by message_id, drops the row of every deleted message
// (FTS5 tables do not cascade) and indexes messages that have no row.
func migrateMessageSearch(db *sql.DB) error {
	legacy, err := hasColumn(db, "message_fts", "message_id")
	if err != nil {
		return err
	}
	if legacy {
		if _, err := db.Exec(`DROP TABLE message_fts`); err != nil {
			return fmt.Errorf("store: drop legacy message search: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS message_fts USING fts5(role, content, provider, model, tokenize = 'unicode61')`); err != nil {
		return fmt.Errorf("store: create message search: %w", err)
	}
	if _, err := db.Exec(`
CREATE TRIGGER IF NOT EXISTS messages_search_delete AFTER DELETE ON messages BEGIN
    DELETE FROM message_fts WHERE rowid = old.seq;
END`); err != nil {
		return fmt.Errorf("store: create message search delete trigger: %w", err)
	}
	return indexUnsearchedMessages(db)
}

// indexUnsearchedMessages indexes messages without a search row, such as
// streamed snapshots whose final update never arrived.
func indexUnsearchedMessages(db *sql.DB) error {
	rows, err := db.Query(`SELECT seq FROM messages WHERE seq NOT IN (SELECT rowid FROM message_fts) ORDER BY seq`)
	if err != nil {
		return fmt.Errorf("store: list unsearched messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var pending []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			return fmt.Errorf("store: scan unsearched message: %w", err)
		}
		pending = append(pending, seq)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate unsearched messages: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin message search backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, seq := range pending {
		message, err := scanMessage(tx.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE seq = ?`, seq))
		if err != nil {
			return fmt.Errorf("store: read message for search backfill: %w", err)
		}
		if err := upsertMessageSearch(ctx, tx, message); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit message search backfill: %w", err)
	}
	return nil
}
