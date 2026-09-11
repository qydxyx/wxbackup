// Package devicesession implements domain.DeviceSession adapters.
//
// Sidecar watches a user-configured official WeChat Backup folder
// (Backup.db + BAK_*). FakeSession is the in-process test double.
// There is no WeChat protocol client and no UI-automation binary here.
package devicesession

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/domain"
)

// Sidecar is a DeviceSession that watches a user-configured directory for an
// official Windows/Mac WeChat backup package (Backup.db + BAK_*). It does not
// speak the WeChat protocol and does not drive a UI-automation binary.
//
// StartBackup succeeds only when backupfmt.Read returns a decoded interchange
// snapshot. Encrypted SQLCipher Backup.db (ErrNeedsKey) fails the job instead
// of ingesting an empty snapshot.
type Sidecar struct {
	Dir string
	// PollInterval is how often the directory is scanned. Zero means 200ms.
	PollInterval time.Duration
	// StableFor is how long Backup.db and BAK_* sizes must stay unchanged
	// before the directory is opened. Zero means 400ms.
	StableFor time.Duration
}

func NewSidecar(dir string) *Sidecar {
	return &Sidecar{Dir: dir}
}

// Resolver returns a backup.Service session factory. An empty dir yields nil,
// which keeps production backup starts at 503 no_session.
func Resolver(dir string) func(context.Context, domain.Account) (domain.DeviceSession, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	sess := NewSidecar(dir)
	return func(context.Context, domain.Account) (domain.DeviceSession, error) {
		return sess, nil
	}
}

func (s *Sidecar) LoginQR(context.Context) (domain.LoginSession, error) {
	return domain.LoginSession{ID: "sidecar"}, nil
}

func (s *Sidecar) WaitLoggedIn(ctx context.Context) (domain.Account, error) {
	snap, err := s.waitPackage(ctx)
	if err != nil {
		return domain.Account{}, err
	}
	return domain.Account{
		WxID:       snap.WxID,
		LoginState: domain.LoginStateLoggedIn,
		BackupRoot: s.Dir,
	}, nil
}

func (s *Sidecar) StartBackup(ctx context.Context, req domain.BackupRequest) (domain.BackupStream, error) {
	if s == nil || strings.TrimSpace(s.Dir) == "" {
		return nil, fmt.Errorf("devicesession: sidecar directory is required")
	}
	if st, err := os.Stat(s.Dir); err == nil && !st.IsDir() {
		return nil, fmt.Errorf("devicesession: sidecar path is not a directory")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("devicesession: sidecar dir: %w", err)
	}
	snap, err := s.waitPackage(ctx)
	if err != nil {
		return nil, err
	}
	fake := &FakeSession{Chunks: snapshotChunks(snap)}
	return fake.StartBackup(ctx, req)
}

func (s *Sidecar) StartRestore(context.Context, domain.RestoreRequest) (domain.RestoreStream, error) {
	return nil, ErrUnsupported
}

func (s *Sidecar) RefreshContacts(context.Context) error { return nil }
func (s *Sidecar) Close(context.Context) error           { return nil }

var _ domain.DeviceSession = (*Sidecar)(nil)

func (s *Sidecar) waitPackage(ctx context.Context) (backupfmt.Snapshot, error) {
	poll := s.PollInterval
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	stableFor := s.StableFor
	if stableFor <= 0 {
		stableFor = 400 * time.Millisecond
	}

	var last string
	var lastSet bool
	var stableSince time.Time

	try := func() (backupfmt.Snapshot, bool, error) {
		fp, ready := fingerprint(s.Dir)
		if !ready {
			return backupfmt.Snapshot{}, false, nil
		}
		now := time.Now()
		if !lastSet || fp != last {
			last = fp
			lastSet = true
			stableSince = now
			return backupfmt.Snapshot{}, false, nil
		}
		if now.Sub(stableSince) < stableFor {
			return backupfmt.Snapshot{}, false, nil
		}
		// Open only after sizes settle so we do not SQLITE_BUSY a writer.
		snap, err := backupfmt.Read(s.Dir)
		if err == nil {
			return snap, true, nil
		}
		if isBusy(err) {
			lastSet = false
			return snap, false, nil
		}
		// Encrypted, truncated, or not an interchange package: fail visibly.
		return snap, false, err
	}

	if snap, ok, err := try(); ok {
		return snap, nil
	} else if err != nil {
		return snap, err
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return backupfmt.Snapshot{}, domain.ErrBackupCancelled
			}
			return backupfmt.Snapshot{}, ctx.Err()
		case <-tick.C:
			if snap, ok, err := try(); ok {
				return snap, nil
			} else if err != nil {
				return snap, err
			}
		}
	}
}

func fingerprint(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var parts []string
	hasDB := false
	hasBAK := false
	busy := false
	for _, e := range entries {
		name := e.Name()
		switch {
		case name == backupfmt.BackupDBName:
			hasDB = true
		case strings.HasSuffix(name, "-wal"), strings.HasSuffix(name, "-shm"), strings.HasSuffix(name, "-journal"):
			busy = true
			continue
		case strings.HasPrefix(name, "BAK_"):
			hasBAK = true
		default:
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%d", name, info.Size(), info.ModTime().UnixNano()))
	}
	if !hasDB || !hasBAK || busy {
		return "", false
	}
	sort.Strings(parts)
	return strings.Join(parts, ","), true
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "busy") || strings.Contains(msg, "locked")
}

func snapshotChunks(snap backupfmt.Snapshot) []Chunk {
	type acc struct {
		ch Chunk
	}
	byTalker := map[string]*acc{}
	add := func(talker string) *acc {
		a, ok := byTalker[talker]
		if !ok {
			a = &acc{ch: Chunk{TalkerID: talker}}
			byTalker[talker] = a
		}
		return a
	}
	for _, c := range snap.Conversations {
		if c.TalkerID == "" {
			continue
		}
		a := add(c.TalkerID)
		a.ch.Conversations = append(a.ch.Conversations, c)
	}
	for _, m := range snap.Messages {
		if m.TalkerID == "" {
			continue
		}
		a := add(m.TalkerID)
		a.ch.Messages = append(a.ch.Messages, m)
		if m.Text != "" {
			a.ch.Bytes += int64(len(m.Text))
		} else {
			a.ch.Bytes++
		}
	}
	talkers := make([]string, 0, len(byTalker))
	for id := range byTalker {
		talkers = append(talkers, id)
	}
	sort.Strings(talkers)
	out := make([]Chunk, 0, len(talkers)+1)
	for _, id := range talkers {
		ch := byTalker[id].ch
		if ch.Bytes == 0 {
			ch.Bytes = 1
		}
		out = append(out, ch)
	}
	if len(snap.Media) > 0 || len(snap.MediaBlobs) > 0 {
		media := Chunk{Media: snap.Media, MediaBlobs: snap.MediaBlobs}
		for _, blob := range snap.MediaBlobs {
			media.Bytes += int64(len(blob))
		}
		if media.Bytes == 0 {
			media.Bytes = 1
		}
		out = append(out, media)
	}
	if len(out) == 0 {
		return []Chunk{{Bytes: 1}}
	}
	return out
}
