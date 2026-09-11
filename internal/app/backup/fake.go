package backup

import (
	"context"
	"sync"

	"github.com/wxbackup/wxbackup/internal/domain"
)

// Chunk is one synthetic talker segment from a DeviceSession BackupStream.
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

// ChunkSource is an optional BackupStream extension used by the fake adapter
// (and tests) until a real LAN session exists.
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
}

func (f *FakeSession) LoginQR(context.Context) (domain.LoginSession, error) {
	return domain.LoginSession{ID: "fake"}, nil
}

func (f *FakeSession) WaitLoggedIn(context.Context) (domain.Account, error) {
	return f.Account, nil
}

func (f *FakeSession) StartBackup(ctx context.Context, _ domain.BackupRequest) (domain.BackupStream, error) {
	if f.StartErr != nil {
		return nil, f.StartErr
	}
	ctx, cancel := context.WithCancel(ctx)
	st := &fakeStream{
		progress: make(chan domain.BackupProgress, 8),
		chunks:   make(chan Chunk, len(f.Chunks)+1),
		stop:     cancel,
		done:     make(chan struct{}),
	}
	go func() {
		defer close(st.done)
		defer close(st.progress)
		defer close(st.chunks)
		defer cancel()
		var bytes int64
		for i, c := range f.Chunks {
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

func (f *FakeSession) StartRestore(context.Context, domain.RestoreRequest) (domain.RestoreStream, error) {
	return nil, ErrNoSession
}

func (f *FakeSession) RefreshContacts(context.Context) error { return nil }
func (f *FakeSession) Close(context.Context) error           { return nil }

var (
	_ domain.DeviceSession = (*FakeSession)(nil)
	_ ChunkSource          = (*fakeStream)(nil)
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

var errFakeFail = domain.ErrPhoneNotForeground
