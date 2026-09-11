package sqlite

import (
	"context"
	"database/sql"
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
	_, err := s.meta.ExecContext(ctx, `
		INSERT INTO accounts (id, wxid, nickname, avatar, login_state, backup_root, access_pwd_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			wxid=excluded.wxid,
			nickname=excluded.nickname,
			avatar=excluded.avatar,
			login_state=excluded.login_state,
			backup_root=excluded.backup_root,
			access_pwd_hash=excluded.access_pwd_hash
	`, a.ID, a.WxID, a.Nickname, a.Avatar, string(a.LoginState), a.BackupRoot, a.AccessPwdHash)
	return err
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
	acct, err := s.GetAccountByWxID(ctx, wxid)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if a, ok := s.accts[wxid]; ok {
		if a != nil && a.db != nil {
			_ = a.db.Close()
			a.db = nil
		}
		delete(s.accts, wxid)
	}
	s.mu.Unlock()
	if _, err := s.meta.ExecContext(ctx, `DELETE FROM backup_jobs WHERE account_id = ?`, acct.ID); err != nil {
		return err
	}
	res, err := s.meta.ExecContext(ctx, `DELETE FROM accounts WHERE wxid = ?`, wxid)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return os.RemoveAll(filepath.Join(s.dataDir, accountsDir, wxid))
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
	return err
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

const accountSelect = `SELECT id, wxid, nickname, avatar, login_state, backup_root, access_pwd_hash FROM accounts`

const jobSelect = `SELECT id, account_id, mode, status, bytes_in, sessions_done, error FROM backup_jobs`

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
