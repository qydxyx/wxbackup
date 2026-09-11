// Package fts indexes message text with SQLite FTS5 in each account's canonical.db.
package fts

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
)

const (
	defaultLimit = 50
	maxLimit     = 1000
)

var ensureStmts = []string{
	`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
		talker_id UNINDEXED,
		msg_id UNINDEXED,
		msg_type UNINDEXED,
		create_time UNINDEXED,
		text,
		tokenize = 'unicode61'
	)`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
		INSERT INTO messages_fts(talker_id, msg_id, msg_type, create_time, text)
		VALUES (new.talker_id, new.msg_id, new.msg_type, new.create_time, new.text);
	END`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
		DELETE FROM messages_fts WHERE talker_id = old.talker_id AND msg_id = old.msg_id;
	END`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE ON messages BEGIN
		DELETE FROM messages_fts WHERE talker_id = old.talker_id AND msg_id = old.msg_id;
		INSERT INTO messages_fts(talker_id, msg_id, msg_type, create_time, text)
		VALUES (new.talker_id, new.msg_id, new.msg_type, new.create_time, new.text);
	END`,
}

// Query is a full-text search over one account's messages.
type Query struct {
	Q       string
	MsgType *int
	From    *time.Time
	To      *time.Time
	Limit   int
}

func Ensure(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("fts: db is required")
	}
	for _, stmt := range ensureStmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("fts: ensure: %w", err)
		}
	}
	return nil
}

// Rebuild replaces the FTS5 index from messages. Tests use this; production
// may call it from a background job via RebuildAsync.
func Rebuild(ctx context.Context, db *sql.DB) error {
	if err := Ensure(ctx, db); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages_fts`); err != nil {
		return fmt.Errorf("fts: rebuild: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages_fts(talker_id, msg_id, msg_type, create_time, text)
		SELECT talker_id, msg_id, msg_type, create_time, text FROM messages
	`); err != nil {
		return fmt.Errorf("fts: rebuild: %w", err)
	}
	return tx.Commit()
}

// RebuildAsync runs Rebuild in the background. Search still backfills
// synchronously when the index is empty so tests do not race.
func RebuildAsync(db *sql.DB) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- Rebuild(context.Background(), db)
	}()
	return done
}

func Search(ctx context.Context, db *sql.DB, q Query) ([]domain.Message, error) {
	if db == nil {
		return nil, fmt.Errorf("fts: db is required")
	}
	needle := strings.TrimSpace(q.Q)
	if needle == "" {
		return []domain.Message{}, nil
	}
	if err := Ensure(ctx, db); err != nil {
		return nil, err
	}
	need, err := needsRebuild(ctx, db)
	if err != nil {
		return nil, err
	}
	if need {
		if err := Rebuild(ctx, db); err != nil {
			return nil, err
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	fromMS := int64(0)
	if q.From != nil && !q.From.IsZero() {
		fromMS = q.From.UnixMilli()
	}
	toMS := int64(math.MaxInt64)
	if q.To != nil && !q.To.IsZero() {
		toMS = q.To.UnixMilli()
	}
	match := quotePhrase(needle)
	if match == "" {
		return []domain.Message{}, nil
	}
	sqlStr := `
		SELECT m.talker_id, m.msg_id, m.account_id, m.msg_seq, m.msg_type, m.is_send, m.create_time, m.text, m.xml, m.extra
		FROM messages_fts
		INNER JOIN messages m ON m.talker_id = messages_fts.talker_id AND m.msg_id = messages_fts.msg_id
		WHERE messages_fts MATCH ?
		  AND m.create_time >= ?
		  AND m.create_time <= ?`
	args := []any{match, fromMS, toMS}
	if q.MsgType != nil {
		sqlStr += ` AND m.msg_type = ?`
		args = append(args, *q.MsgType)
	}
	sqlStr += `
		ORDER BY m.create_time DESC, m.msg_seq DESC, m.msg_id DESC
		LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("fts: search: %w", err)
	}
	defer rows.Close()
	out := []domain.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		// MATCH is a candidate generator; keep literal phrase hits only so a
		// neutralized * cannot still surface prefix-expanded tokens.
		if !containsFold(m.Text, needle) {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func needsRebuild(ctx context.Context, db *sql.DB) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM messages_fts LIMIT 1`).Scan(&n)
	if err == nil {
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	err = db.QueryRowContext(ctx, `SELECT 1 FROM messages LIMIT 1`).Scan(&n)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}

// quotePhrase treats the user string as a literal phrase so MATCH is not a
// query-language injection surface (AND/OR/NEAR/"/*).
func quotePhrase(q string) string {
	q = strings.TrimSpace(q)
	// FTS5 still treats a trailing * inside quotes as a prefix token.
	q = strings.ReplaceAll(q, "*", " ")
	q = strings.ReplaceAll(q, `"`, `""`)
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	return `"` + q + `"`
}

func containsFold(text, sub string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(sub))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessage(sc rowScanner) (domain.Message, error) {
	var m domain.Message
	var isSend int
	var created int64
	var extra string
	if err := sc.Scan(&m.TalkerID, &m.MsgID, &m.AccountID, &m.MsgSeq, &m.MsgType, &isSend, &created, &m.Text, &m.XML, &extra); err != nil {
		return domain.Message{}, err
	}
	m.IsSend = isSend != 0
	m.CreateTime = time.UnixMilli(created).UTC()
	if extra == "" {
		return m, nil
	}
	if err := json.Unmarshal([]byte(extra), &m.Extra); err != nil {
		return domain.Message{}, err
	}
	return m, nil
}
