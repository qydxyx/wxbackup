package domain

import (
	"context"
	"time"
)

// DeviceSession is the NAS-side computer login used to drive backup/restore.
type DeviceSession interface {
	LoginQR(ctx context.Context) (LoginSession, error)
	WaitLoggedIn(ctx context.Context) (Account, error)
	StartBackup(ctx context.Context, req BackupRequest) (BackupStream, error)
	StartRestore(ctx context.Context, req RestoreRequest) (RestoreStream, error)
	RefreshContacts(ctx context.Context) error
	Close(ctx context.Context) error
}

type LoginSession struct {
	ID        string    `json:"id"`
	QR        []byte    `json:"-"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

type BackupRequest struct {
	AccountID string         `json:"account_id"`
	Mode      BackupMode     `json:"mode"`
	TalkerIDs []string       `json:"talker_ids,omitempty"`
	Cursors   []BackupCursor `json:"cursors,omitempty"`
}

type RestoreRequest struct {
	AccountID string          `json:"account_id"`
	Selector  RestoreSelector `json:"selector"`
}

type BackupProgress struct {
	BytesIn      int64     `json:"bytes_in"`
	SessionsDone int       `json:"sessions_done"`
	Status       JobStatus `json:"status"`
}

type RestoreProgress struct {
	SessionsDone int       `json:"sessions_done"`
	Status       JobStatus `json:"status"`
}

type BackupStream interface {
	Progress() <-chan BackupProgress
	Wait() error
	Cancel(ctx context.Context) error
}

type RestoreStream interface {
	Progress() <-chan RestoreProgress
	Wait() error
	Cancel(ctx context.Context) error
}
