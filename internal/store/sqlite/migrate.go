package sqlite

import (
	"database/sql"
	"fmt"
	"time"
)

type migration []string

var metaMigrations = []migration{
	{
		`CREATE TABLE accounts (
			id TEXT PRIMARY KEY,
			wxid TEXT NOT NULL UNIQUE,
			nickname TEXT NOT NULL DEFAULT '',
			avatar TEXT NOT NULL DEFAULT '',
			login_state TEXT NOT NULL,
			backup_root TEXT NOT NULL DEFAULT '',
			access_pwd_hash TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE backup_jobs (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			mode TEXT NOT NULL,
			status TEXT NOT NULL,
			bytes_in INTEGER NOT NULL DEFAULT 0,
			sessions_done INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX backup_jobs_account ON backup_jobs (account_id)`,
	},
	{
		`CREATE TABLE restore_jobs (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			selector_kind TEXT NOT NULL,
			session_ids TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			sessions_done INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX restore_jobs_account ON restore_jobs (account_id)`,
	},
}

var accountMigrations = []migration{
	{
		`CREATE TABLE conversations (
			talker_id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			avatar TEXT NOT NULL DEFAULT '',
			last_msg_time INTEGER NOT NULL DEFAULT 0,
			msg_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE messages (
			talker_id TEXT NOT NULL,
			msg_id TEXT NOT NULL,
			account_id TEXT NOT NULL,
			msg_seq INTEGER NOT NULL DEFAULT 0,
			msg_type INTEGER NOT NULL DEFAULT 0,
			is_send INTEGER NOT NULL DEFAULT 0,
			create_time INTEGER NOT NULL,
			text TEXT NOT NULL DEFAULT '',
			xml TEXT NOT NULL DEFAULT '',
			extra TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (talker_id, msg_id)
		)`,
		`CREATE INDEX messages_talker_time ON messages (talker_id, create_time DESC, msg_seq DESC, msg_id DESC)`,
		`CREATE TABLE media (
			media_id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			sha256 TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			available INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE backup_cursors (
			talker_id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			last_end_time INTEGER NOT NULL DEFAULT 0,
			segment_meta TEXT NOT NULL DEFAULT '',
			received INTEGER NOT NULL DEFAULT 0,
			total INTEGER NOT NULL DEFAULT 0
		)`,
	},
}

func applyMigrations(db *sql.DB, ms []migration) error {
	// Version table is created outside numbered steps so Open is safe to call again.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return err
	}
	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	for i, m := range ms {
		v := i + 1
		if v <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range m {
			if _, err := tx.Exec(stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("sqlite: migration %d: %w", v, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, v, time.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: migration %d: %w", v, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func schemaVersion(db *sql.DB) (int, error) {
	var v int
	err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	return v, err
}
