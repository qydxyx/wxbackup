// Package stats derives per-account session and message totals from canonical.db.
package stats

import (
	"context"
	"database/sql"
	"fmt"
)

// Snapshot is live volume for one account: sessions, messages, and type histogram.
// Counts come from conversations/messages, not a yearly recap table.
type Snapshot struct {
	AccountID    string      `json:"account_id"`
	SessionCount int64       `json:"session_count"`
	MessageCount int64       `json:"message_count"`
	Types        []TypeCount `json:"types"`
}

// TypeCount is how many messages have a given msg_type.
type TypeCount struct {
	MsgType int   `json:"msg_type"`
	Count   int64 `json:"count"`
}

// Account aggregates conversations and messages in one account's canonical.db.
func Account(ctx context.Context, db *sql.DB) (Snapshot, error) {
	if db == nil {
		return Snapshot{}, fmt.Errorf("stats: db is required")
	}
	out := Snapshot{Types: []TypeCount{}}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations`).Scan(&out.SessionCount); err != nil {
		return Snapshot{}, fmt.Errorf("stats: session_count: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&out.MessageCount); err != nil {
		return Snapshot{}, fmt.Errorf("stats: message_count: %w", err)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT msg_type, COUNT(*)
		FROM messages
		GROUP BY msg_type
		ORDER BY msg_type
	`)
	if err != nil {
		return Snapshot{}, fmt.Errorf("stats: types: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t TypeCount
		if err := rows.Scan(&t.MsgType, &t.Count); err != nil {
			return Snapshot{}, fmt.Errorf("stats: types: %w", err)
		}
		out.Types = append(out.Types, t)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("stats: types: %w", err)
	}
	return out, nil
}
