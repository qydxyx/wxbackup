package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wxbackup/wxbackup/internal/domain"
)

func (s *Store) PutAccount(ctx context.Context, a domain.Account) error {
	if a.ID == "" || a.WxID == "" {
		return fmt.Errorf("sqlite: account id and wxid are required")
	}
	if err := validWxID(a.WxID); err != nil {
		return err
	}
	if !a.LoginState.Valid() {
		return fmt.Errorf("sqlite: invalid login state %q", a.LoginState)
	}
	existing, err := s.GetAccount(ctx, a.ID)
	if err == nil && existing.WxID != a.WxID {
		return fmt.Errorf("%w: account %s wxid is immutable (%q)", ErrConstraint, a.ID, existing.WxID)
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = s.meta.ExecContext(ctx, `
		INSERT INTO accounts (id, wxid, nickname, avatar, login_state, backup_root, access_pwd_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			nickname=excluded.nickname,
			avatar=excluded.avatar,
			login_state=excluded.login_state,
			backup_root=excluded.backup_root,
			access_pwd_hash=excluded.access_pwd_hash
	`, a.ID, a.WxID, a.Nickname, a.Avatar, string(a.LoginState), a.BackupRoot, a.AccessPwdHash)
	return mapSQLError(err)
}

func (s *Store) GetAccount(ctx context.Context, id string) (domain.Account, error) {
	return scanAccount(s.meta.QueryRowContext(ctx, accountSelect+` WHERE id = ?`, id))
}

func (s *Store) GetAccountByWxID(ctx context.Context, wxid string) (domain.Account, error) {
	return scanAccount(s.meta.QueryRowContext(ctx, accountSelect+` WHERE wxid = ?`, wxid))
}

func (s *Store) ListAccounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := s.meta.QueryContext(ctx, accountSelect+` ORDER BY wxid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAccount(ctx context.Context, wxid string) error {
	if err := validWxID(wxid); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acct, err := s.GetAccountByWxID(ctx, wxid)
	if err != nil {
		return err
	}
	if a, ok := s.accts[wxid]; ok {
		if a != nil && a.db != nil {
			if err := a.db.Close(); err != nil {
				return err
			}
			a.db = nil
		}
		delete(s.accts, wxid)
	}
	// Drop files first so a failed RemoveAll leaves the meta row (library still reachable).
	if err := os.RemoveAll(filepath.Join(s.dataDir, accountsDir, wxid)); err != nil {
		return err
	}
	tx, err := s.meta.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_jobs WHERE account_id = ?`, acct.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM restore_jobs WHERE account_id = ?`, acct.ID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM accounts WHERE wxid = ?`, wxid)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) PutBackupJob(ctx context.Context, j domain.BackupJob) error {
	if j.ID == "" || j.AccountID == "" {
		return fmt.Errorf("sqlite: backup job id and account_id are required")
	}
	if !j.Mode.Valid() {
		return fmt.Errorf("sqlite: invalid backup mode %q", j.Mode)
	}
	if !j.Status.Valid() {
		return fmt.Errorf("sqlite: invalid job status %q", j.Status)
	}
	_, err := s.meta.ExecContext(ctx, `
		INSERT INTO backup_jobs (id, account_id, mode, status, bytes_in, sessions_done, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_id=excluded.account_id,
			mode=excluded.mode,
			status=excluded.status,
			bytes_in=excluded.bytes_in,
			sessions_done=excluded.sessions_done,
			error=excluded.error
	`, j.ID, j.AccountID, string(j.Mode), string(j.Status), j.BytesIn, j.SessionsDone, j.Error)
	return mapSQLError(err)
}

func (s *Store) GetBackupJob(ctx context.Context, id string) (domain.BackupJob, error) {
	return scanJob(s.meta.QueryRowContext(ctx, jobSelect+` WHERE id = ?`, id))
}

func (s *Store) ListBackupJobs(ctx context.Context, accountID string) ([]domain.BackupJob, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if accountID == "" {
		rows, err = s.meta.QueryContext(ctx, jobSelect+` ORDER BY id`)
	} else {
		rows, err = s.meta.QueryContext(ctx, jobSelect+` WHERE account_id = ? ORDER BY id`, accountID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BackupJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) PutRestoreJob(ctx context.Context, j domain.RestoreJob) error {
	if j.ID == "" || j.AccountID == "" {
		return fmt.Errorf("sqlite: restore job id and account_id are required")
	}
	if !j.Selector.Kind.Valid() {
		return fmt.Errorf("sqlite: invalid restore selector %q", j.Selector.Kind)
	}
	if !j.Status.Valid() {
		return fmt.Errorf("sqlite: invalid job status %q", j.Status)
	}
	ids, err := marshalSessionIDs(j.Selector.SessionIDs)
	if err != nil {
		return err
	}
	_, err = s.meta.ExecContext(ctx, `
		INSERT INTO restore_jobs (id, account_id, selector_kind, session_ids, status, sessions_done, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_id=excluded.account_id,
			selector_kind=excluded.selector_kind,
			session_ids=excluded.session_ids,
			status=excluded.status,
			sessions_done=excluded.sessions_done,
			error=excluded.error
	`, j.ID, j.AccountID, string(j.Selector.Kind), ids, string(j.Status), j.SessionsDone, j.Error)
	return mapSQLError(err)
}

func (s *Store) GetRestoreJob(ctx context.Context, id string) (domain.RestoreJob, error) {
	return scanRestoreJob(s.meta.QueryRowContext(ctx, restoreJobSelect+` WHERE id = ?`, id))
}

func (s *Store) ListRestoreJobs(ctx context.Context, accountID string) ([]domain.RestoreJob, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if accountID == "" {
		rows, err = s.meta.QueryContext(ctx, restoreJobSelect+` ORDER BY id`)
	} else {
		rows, err = s.meta.QueryContext(ctx, restoreJobSelect+` WHERE account_id = ? ORDER BY id`, accountID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RestoreJob
	for rows.Next() {
		j, err := scanRestoreJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

const accountSelect = `SELECT id, wxid, nickname, avatar, login_state, backup_root, access_pwd_hash FROM accounts`

const jobSelect = `SELECT id, account_id, mode, status, bytes_in, sessions_done, error FROM backup_jobs`

const restoreJobSelect = `SELECT id, account_id, selector_kind, session_ids, status, sessions_done, error FROM restore_jobs`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAccount(sc rowScanner) (domain.Account, error) {
	var a domain.Account
	var state string
	err := sc.Scan(&a.ID, &a.WxID, &a.Nickname, &a.Avatar, &state, &a.BackupRoot, &a.AccessPwdHash)
	if err != nil {
		return domain.Account{}, mapNotFound(err)
	}
	a.LoginState = domain.LoginState(state)
	return a, nil
}

func scanJob(sc rowScanner) (domain.BackupJob, error) {
	var j domain.BackupJob
	var mode, status string
	err := sc.Scan(&j.ID, &j.AccountID, &mode, &status, &j.BytesIn, &j.SessionsDone, &j.Error)
	if err != nil {
		return domain.BackupJob{}, mapNotFound(err)
	}
	j.Mode = domain.BackupMode(mode)
	j.Status = domain.JobStatus(status)
	return j, nil
}

func scanRestoreJob(sc rowScanner) (domain.RestoreJob, error) {
	var j domain.RestoreJob
	var kind, ids, status string
	err := sc.Scan(&j.ID, &j.AccountID, &kind, &ids, &status, &j.SessionsDone, &j.Error)
	if err != nil {
		return domain.RestoreJob{}, mapNotFound(err)
	}
	j.Selector.Kind = domain.RestoreSelectorKind(kind)
	j.Selector.SessionIDs, err = unmarshalSessionIDs(ids)
	if err != nil {
		return domain.RestoreJob{}, err
	}
	j.Status = domain.JobStatus(status)
	return j, nil
}

func marshalSessionIDs(ids []string) (string, error) {
	if len(ids) == 0 {
		return "", nil
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalSessionIDs(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}
