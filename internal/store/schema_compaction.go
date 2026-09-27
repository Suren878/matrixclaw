package store

import (
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// Text prefixes older versions gave the system messages that marked a
// compaction or a /clear.
const (
	legacyCompactPrefix = "🧠 Context compacted"
	legacyClearPrefix   = "🧹 Context cleared"
)

var legacyCompactStats = regexp.MustCompile(`~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s*->\s*~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s+tokens`)

// migrateCompactionMarkers adds messages.compaction_json and turns the text
// markers older versions wrote into boundaries; such a marker covered every
// message of its session written before it.
func migrateCompactionMarkers(db *sql.DB) error {
	if err := ensureColumn(db, "messages", "compaction_json", `ALTER TABLE messages ADD COLUMN compaction_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT id, seq, content FROM messages WHERE role = 'system' AND compaction_json = '' AND (content LIKE ? OR content LIKE ?)`, legacyCompactPrefix+"%", legacyClearPrefix+"%")
	if err != nil {
		return fmt.Errorf("store: list legacy context markers: %w", err)
	}
	updates := map[string]string{}
	for rows.Next() {
		var id, content string
		var seq int64
		if err := rows.Scan(&id, &seq, &content); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: scan legacy context marker: %w", err)
		}
		if compaction, ok := legacyCompaction(seq, content); ok {
			updates[id] = marshalCompaction(&compaction)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("store: iterate legacy context markers: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("store: close legacy context markers: %w", err)
	}
	for id, body := range updates {
		if _, err := db.Exec(`UPDATE messages SET compaction_json = ? WHERE id = ?`, body, id); err != nil {
			return fmt.Errorf("store: migrate legacy context marker: %w", err)
		}
	}
	return nil
}

func legacyCompaction(seq int64, content string) (transcript.Compaction, bool) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, legacyClearPrefix) {
		return transcript.Compaction{CoversThroughSeq: seq - 1, Cleared: true}, true
	}
	rest, ok := strings.CutPrefix(content, legacyCompactPrefix)
	if !ok {
		return transcript.Compaction{}, false
	}
	compaction := transcript.Compaction{CoversThroughSeq: seq - 1}
	rest = strings.TrimSpace(rest)
	if header, summary, found := strings.Cut(rest, "\n\n"); found && strings.HasPrefix(header, ":") {
		if match := legacyCompactStats.FindStringSubmatch(header); match != nil {
			compaction.TokensBefore = legacyTokenCount(match[1])
			compaction.TokensAfter = legacyTokenCount(match[2])
		}
		rest = summary
	}
	compaction.Summary = strings.TrimSpace(rest)
	return compaction, true
}

func legacyTokenCount(value string) int {
	value = strings.ToLower(strings.TrimSpace(value))
	multiplier := 1.0
	switch {
	case strings.HasSuffix(value, "k"):
		multiplier, value = 1_000, strings.TrimSuffix(value, "k")
	case strings.HasSuffix(value, "m"):
		multiplier, value = 1_000_000, strings.TrimSuffix(value, "m")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return int(math.Round(parsed * multiplier))
}
