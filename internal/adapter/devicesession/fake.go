package devicesession

import (
	"context"
	"errors"
	"sync"

	"github.com/wxbackup/wxbackup/internal/domain"
)

// ErrUnsupported is returned for DeviceSession methods this adapter does not implement.
var ErrUnsupported = errors.New("devicesession: operation not supported")

// Chunk is one talker segment from a DeviceSession BackupStream.
type Chunk struct {
	TalkerID      string
	Conversations []domain.Conversation
	Messages      []domain.Message
	Media         []domain.MediaObject
	MediaBlobs    map[string][]byte
	SegmentMeta   string
	Received      int64
	Total         int64
	Bytes         int64
}

// ChunkSource is an optional BackupStream extension used by the sidecar and
// fake adapters. backup.Service merges these into a backupfmt snapshot.
type ChunkSource interface {
	Chunks() <-chan Chunk
}

// FakeSession is a DeviceSession that yields synthetic chunks. It does not
// speak WeChat LAN protocol.
type FakeSession struct {
	Account  domain.Account
	Chunks   []Chunk
	StartErr error
	WaitErr  error
	// FailAfter, if > 0, stops after that many chunks and fails the stream.
	FailAfter int
	// Hold, if set, blocks completion until Hold is done or the job is cancelled.
	Hold context.Context
	// Holding is closed once the stream is blocked on Hold (test sync).
	Holding chan struct{}

	mu          sync.Mutex
	holdOnce    sync.Once
	LastRequest domain.BackupRequest
	LastRestore domain.RestoreRequest
	Emitted     []string
	// OutDir, if set, is the official Backup.db destination used by restore jobs.
	OutDir string
	// RestoreTalkers is the session list StartRestore reports. Empty uses the request selector.
	RestoreTalkers []string
}

func (f *FakeSession) LoginQR(context.Context) (domain.LoginSession, error) {
	return domain.LoginSession{ID: "fake"}, nil
}

func (f *FakeSession) WaitLoggedIn(context.Context) (domain.Account, error) {
	return f.Account, nil
}

func (f *FakeSession) StartBackup(ctx context.Context, req domain.BackupRequest) (domain.BackupStream, error) {
	if f.StartErr != nil {
		return nil, f.StartErr
	}
	f.mu.Lock()
	f.LastRequest = req
	chunks := filterChunks(append([]Chunk(nil), f.Chunks...), req)
	f.Emitted = f.Emitted[:0]
	for _, c := range chunks {
		f.Emitted = append(f.Emitted, c.TalkerID)
	}
	f.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	st := &fakeStream{
		progress: make(chan domain.BackupProgress, 8),
		chunks:   make(chan Chunk, len(chunks)+1),
		stop:     cancel,
		done:     make(chan struct{}),
	}
	go func() {
		defer close(st.done)
		defer close(st.progress)
		defer close(st.chunks)
		defer cancel()
		var bytes int64
		for i, c := range chunks {
			if f.FailAfter > 0 && i >= f.FailAfter {
				st.setErr(f.WaitErr)
				if st.err == nil {
					st.setErr(errFakeFail)
				}
				return
			}
			select {
			case <-ctx.Done():
				st.setErr(domain.ErrBackupCancelled)
				return
			case st.chunks <- c:
				if c.Bytes > 0 {
					bytes += c.Bytes
				} else {
					bytes += 1
				}
				st.progress <- domain.BackupProgress{
					BytesIn:      bytes,
					SessionsDone: i + 1,
					Status:       domain.JobTransfer,
				}
			}
		}
		if f.Hold != nil {
			if f.Holding != nil {
				f.holdOnce.Do(func() { close(f.Holding) })
			}
			select {
			case <-ctx.Done():
				st.setErr(domain.ErrBackupCancelled)
				return
			case <-f.Hold.Done():
			}
		}
		if f.WaitErr != nil && f.FailAfter == 0 {
			st.setErr(f.WaitErr)
		}
	}()
	return st, nil
}

func (f *FakeSession) StartRestore(ctx context.Context, req domain.RestoreRequest) (domain.RestoreStream, error) {
	if f.StartErr != nil {
		return nil, f.StartErr
	}
	f.mu.Lock()
	f.LastRestore = req
	talkers := append([]string(nil), f.RestoreTalkers...)
	f.mu.Unlock()
	talkers = filterRestoreTalkers(talkers, req)
	ctx, cancel := context.WithCancel(ctx)
	st := &fakeRestoreStream{
		progress: make(chan domain.RestoreProgress, 8),
		stop:     cancel,
		done:     make(chan struct{}),
	}
	go func() {
		defer close(st.done)
		defer close(st.progress)
		defer cancel()
		for i := range talkers {
			if f.FailAfter > 0 && i >= f.FailAfter {
				st.setErr(f.WaitErr)
				if st.err == nil {
					st.setErr(errFakeFail)
				}
				return
			}
			select {
			case <-ctx.Done():
				st.setErr(domain.ErrBackupCancelled)
				return
			case st.progress <- domain.RestoreProgress{
				SessionsDone: i + 1,
				Status:       domain.JobTransfer,
			}:
			}
		}
		if f.Hold != nil {
			if f.Holding != nil {
				f.holdOnce.Do(func() { close(f.Holding) })
			}
			select {
			case <-ctx.Done():
				st.setErr(domain.ErrBackupCancelled)
				return
			case <-f.Hold.Done():
			}
		}
		if f.WaitErr != nil && f.FailAfter == 0 {
			st.setErr(f.WaitErr)
		}
	}()
	return st, nil
}

func (f *FakeSession) RestoreDir() string {
	if f == nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.OutDir
}

func (f *FakeSession) RefreshContacts(context.Context) error { return nil }
func (f *FakeSession) Close(context.Context) error           { return nil }

func (f *FakeSession) LastBackupRequest() domain.BackupRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.LastRequest
}

func (f *FakeSession) LastRestoreRequest() domain.RestoreRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.LastRestore
}

func (f *FakeSession) EmittedTalkers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.Emitted...)
}

var (
	_ domain.DeviceSession = (*FakeSession)(nil)
	_ ChunkSource          = (*fakeStream)(nil)
	_ domain.RestoreStream = (*fakeRestoreStream)(nil)
)

type fakeStream struct {
	progress chan domain.BackupProgress
	chunks   chan Chunk
	stop     context.CancelFunc
	done     chan struct{}

	mu  sync.Mutex
	err error
}

func (s *fakeStream) Progress() <-chan domain.BackupProgress { return s.progress }
func (s *fakeStream) Chunks() <-chan Chunk                   { return s.chunks }

func (s *fakeStream) Wait() error {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *fakeStream) Cancel(context.Context) error {
	s.stop()
	return domain.ErrBackupCancelled
}

func (s *fakeStream) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

type fakeRestoreStream struct {
	progress chan domain.RestoreProgress
	stop     context.CancelFunc
	done     chan struct{}

	mu  sync.Mutex
	err error
}

func (s *fakeRestoreStream) Progress() <-chan domain.RestoreProgress { return s.progress }

func (s *fakeRestoreStream) Wait() error {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *fakeRestoreStream) Cancel(context.Context) error {
	s.stop()
	return domain.ErrBackupCancelled
}

func (s *fakeRestoreStream) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func filterRestoreTalkers(talkers []string, req domain.RestoreRequest) []string {
	if req.Selector.Kind != domain.RestoreSessionIDs {
		return talkers
	}
	want := make(map[string]struct{}, len(req.Selector.SessionIDs))
	for _, id := range req.Selector.SessionIDs {
		if id == "" {
			continue
		}
		want[id] = struct{}{}
	}
	if len(talkers) == 0 {
		out := make([]string, 0, len(req.Selector.SessionIDs))
		for _, id := range req.Selector.SessionIDs {
			if id != "" {
				out = append(out, id)
			}
		}
		return out
	}
	out := make([]string, 0, len(talkers))
	for _, id := range talkers {
		if _, ok := want[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

var errFakeFail = domain.ErrPhoneNotForeground

func filterChunks(chunks []Chunk, req domain.BackupRequest) []Chunk {
	if req.Mode == domain.BackupModeFull || len(req.Cursors) == 0 {
		return chunks
	}
	byTalker := make(map[string]domain.BackupCursor, len(req.Cursors))
	for _, c := range req.Cursors {
		byTalker[c.TalkerID] = c
	}
	out := make([]Chunk, 0, len(chunks))
	for _, ch := range chunks {
		cur, ok := byTalker[ch.TalkerID]
		if !ok {
			out = append(out, ch)
			continue
		}
		kept := ch
		kept.Messages = nil
		for _, m := range ch.Messages {
			if messageAtOrBeforeCursor(cur, m) {
				continue
			}
			kept.Messages = append(kept.Messages, m)
		}
		if len(kept.Messages) == 0 {
			continue
		}
		out = append(out, kept)
	}
	return out
}

func messageAtOrBeforeCursor(cur domain.BackupCursor, m domain.Message) bool {
	if cur.LastEndTime.IsZero() && cur.SegmentMeta == "" {
		return false
	}
	if !cur.LastEndTime.IsZero() {
		if m.CreateTime.Before(cur.LastEndTime) {
			return true
		}
		if m.CreateTime.Equal(cur.LastEndTime) {
			return cur.SegmentMeta == "" || m.MsgID == cur.SegmentMeta
		}
		return false
	}
	return m.MsgID == cur.SegmentMeta
}
