package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	_ "modernc.org/sqlite" // pure Go; CI and NAS builds must not require CGO
)

var (
	ErrNotFound        = errors.New("sqlite: not found")
	ErrConstraint      = errors.New("sqlite: constraint")
	ErrAccountMismatch = errors.New("sqlite: account id mismatch")
)

const (
	metaFile         = "meta.db"
	accountsDir      = "accounts"
	canonicalFile    = "canonical.db"
	busyTimeoutMS    = 5000
	defaultMsgPage   = 50
	maxMsgPage       = 1000
	sqliteConstraint = 19 // SQLITE_CONSTRAINT primary result code
	dirPerm          = 0o700
)

// Jobs live in meta.db; messages never do.
type Store struct {
	dataDir string
	meta    *sql.DB
	mu      sync.Mutex
	accts   map[string]*AccountDB
}

func MetaPath(dataDir string) string {
	return filepath.Join(dataDir, metaFile)
}

func CanonicalPath(dataDir, wxid string) string {
	return filepath.Join(dataDir, accountsDir, wxid, canonicalFile)
}

func Open(dataDir string) (*Store, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("sqlite: data dir is required")
	}
	if err := os.MkdirAll(dataDir, dirPerm); err != nil {
		return nil, err
	}
	meta, err := openDB(MetaPath(dataDir))
	if err != nil {
		return nil, err
	}
	if err := applyMigrations(meta, metaMigrations); err != nil {
		_ = meta.Close()
		return nil, err
	}
	return &Store{dataDir: dataDir, meta: meta, accts: make(map[string]*AccountDB)}, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var first error
	for wxid, a := range s.accts {
		if a != nil && a.db != nil {
			if err := a.db.Close(); err != nil && first == nil {
				first = err
			}
			a.db = nil
		}
		delete(s.accts, wxid)
	}
	if s.meta != nil {
		if err := s.meta.Close(); err != nil && first == nil {
			first = err
		}
		s.meta = nil
	}
	return first
}

func (s *Store) OpenAccount(ctx context.Context, wxid string) (*AccountDB, error) {
	if err := validWxID(wxid); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acct, err := s.GetAccountByWxID(ctx, wxid)
	if err != nil {
		return nil, err
	}
	if a, ok := s.accts[wxid]; ok && a != nil && a.db != nil {
		return a, nil
	}
	dir := filepath.Join(s.dataDir, accountsDir, wxid)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, err
	}
	db, err := openDB(filepath.Join(dir, canonicalFile))
	if err != nil {
		return nil, err
	}
	if err := applyMigrations(db, accountMigrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	a := &AccountDB{wxid: wxid, accountID: acct.ID, db: db}
	s.accts[wxid] = a
	return a, nil
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type AccountDB struct {
	wxid      string
	accountID string
	db        *sql.DB
	tx        *sql.Tx
}

func (a *AccountDB) q() querier {
	if a != nil && a.tx != nil {
		return a.tx
	}
	return a.db
}

// InTx runs fn in a single account-db transaction. Nested calls reuse the tx.
func (a *AccountDB) InTx(ctx context.Context, fn func(*AccountDB) error) error {
	if a.tx != nil {
		return fn(a)
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	scoped := *a
	scoped.tx = tx
	if err := fn(&scoped); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (a *AccountDB) CountMessages(ctx context.Context, talkerID string) (int64, error) {
	var n int64
	err := a.q().QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE talker_id = ?`, talkerID).Scan(&n)
	return n, err
}

func (a *AccountDB) WxID() string      { return a.wxid }
func (a *AccountDB) AccountID() string { return a.accountID }

// Conn is the per-account canonical handle. FTS lives in this file so WAL
// and the single-connection busy timeout cover index rebuilds too.
func (a *AccountDB) Conn() *sql.DB {
	if a == nil {
		return nil
	}
	return a.db
}

func (a *AccountDB) bindAccountID(got string) (string, error) {
	if got != "" && got != a.accountID {
		return "", fmt.Errorf("%w: %q != %q", ErrAccountMismatch, got, a.accountID)
	}
	return a.accountID, nil
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection avoids SQLITE_BUSY from overlapping writers on the same handle.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := enableWAL(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func enableWAL(db *sql.DB) error {
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeoutMS)); err != nil {
		return err
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return err
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("sqlite: journal_mode=%s, want wal", mode)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return err
	}
	return nil
}

func validWxID(wxid string) error {
	if wxid == "" || wxid == "." || wxid == ".." {
		return fmt.Errorf("sqlite: invalid wxid %q", wxid)
	}
	for _, r := range wxid {
		if r == 0 || r == '/' || r == '\\' || unicode.IsControl(r) {
			return fmt.Errorf("sqlite: invalid wxid %q", wxid)
		}
	}
	if filepath.Base(wxid) != wxid {
		return fmt.Errorf("sqlite: invalid wxid %q", wxid)
	}
	return nil
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func mapSQLError(err error) error {
	if err == nil {
		return nil
	}
	var se interface{ Code() int }
	if errors.As(err, &se) && se.Code()&0xff == sqliteConstraint {
		return fmt.Errorf("%w: %v", ErrConstraint, err)
	}
	return err
}
