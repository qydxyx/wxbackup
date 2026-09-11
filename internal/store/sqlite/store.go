package sqlite

import (
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

var ErrNotFound = errors.New("sqlite: not found")

const (
	metaFile       = "meta.db"
	accountsDir    = "accounts"
	canonicalFile  = "canonical.db"
	busyTimeoutMS  = 5000
	defaultMsgPage = 50
	maxMsgPage     = 1000
)

// Store is the process-wide handle for meta.db and per-account canonical.db files.
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
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
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

func (s *Store) OpenAccount(wxid string) (*AccountDB, error) {
	if err := validWxID(wxid); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.accts[wxid]; ok && a != nil && a.db != nil {
		return a, nil
	}
	db, err := openDB(CanonicalPath(s.dataDir, wxid))
	if err != nil {
		return nil, err
	}
	if err := applyMigrations(db, accountMigrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	a := &AccountDB{wxid: wxid, db: db}
	s.accts[wxid] = a
	return a, nil
}

// AccountDB is one account's canonical.db (conversations, messages, media, cursors).
type AccountDB struct {
	wxid string
	db   *sql.DB
}

func (a *AccountDB) WxID() string { return a.wxid }

func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
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
