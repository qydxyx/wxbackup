package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

var (
	ErrNotFound    = errors.New("backup: not found")
	ErrConflict    = errors.New("backup: account already has an active job")
	ErrInvalidMode = errors.New("backup: invalid mode")
	ErrNoSession   = errors.New("backup: device session is not configured")
)

// SessionResolver returns the NAS-side computer login for an account.
// Production wires devicesession.Sidecar when WXBACKUP_SIDECAR_DIR is set;
// tests inject a FakeSession. Nil means Start returns ErrNoSession.
type SessionResolver func(ctx context.Context, account domain.Account) (domain.DeviceSession, error)

type Options struct {
	Store    *sqlite.Store
	DataDir  string
	Sessions SessionResolver
	NewID    func() string
}

type Service struct {
	store    *sqlite.Store
	dataDir  string
	sessions SessionResolver
	newID    func() string

	root   context.Context
	cancel context.CancelFunc

	mu   sync.Mutex
	jobs map[string]*runtimeJob

	// test-only: block organize until ctx is cancelled, or fail after a talker.
	organizeHold func(ctx context.Context) error
	afterTalker  func(talker string) error
}

type runtimeJob struct {
	mu          sync.Mutex
	job         domain.BackupJob
	transitions []domain.JobStatus
	cancel      context.CancelFunc
	stream      domain.BackupStream
	done        chan struct{}
	subscribers []chan domain.BackupJob
}

func New(opt Options) (*Service, error) {
	if opt.Store == nil {
		return nil, errors.New("backup: store is required")
	}
	newID := opt.NewID
	if newID == nil {
		newID = randomID
	}
	root, cancel := context.WithCancel(context.Background())
	s := &Service{
		store:    opt.Store,
		dataDir:  opt.DataDir,
		sessions: opt.Sessions,
		newID:    newID,
		root:     root,
		cancel:   cancel,
		jobs:     make(map[string]*runtimeJob),
	}
	if err := s.recover(root); err != nil {
		cancel()
		return nil, err
	}
	return s, nil
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.cancel()
	s.mu.Lock()
	rts := make([]*runtimeJob, 0, len(s.jobs))
	for _, rt := range s.jobs {
		rts = append(rts, rt)
	}
	s.mu.Unlock()
	for _, rt := range rts {
		<-rt.done
	}
	return nil
}

func (s *Service) recover(ctx context.Context) error {
	jobs, err := s.store.ListBackupJobs(ctx, "")
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Status.Terminal() {
			continue
		}
		j.Status = domain.JobFailed
		j.Error = "interrupted"
		if err := s.store.PutBackupJob(ctx, j); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Start(ctx context.Context, accountID string, mode domain.BackupMode) (domain.BackupJob, error) {
	if mode == "" {
		mode = domain.BackupModeFull
	}
	if !mode.Valid() {
		return domain.BackupJob{}, ErrInvalidMode
	}
	if s.sessions == nil {
		return domain.BackupJob{}, ErrNoSession
	}
	acct, err := s.store.GetAccount(ctx, accountID)
	if err != nil {
		return domain.BackupJob{}, mapStoreErr(err)
	}
	if acct.LoginState != domain.LoginStateLoggedIn {
		return domain.BackupJob{}, domain.ErrNotLoggedIn
	}
	existing, err := s.store.ListBackupJobs(ctx, accountID)
	if err != nil {
		return domain.BackupJob{}, err
	}
	for _, j := range existing {
		if !j.Status.Terminal() {
			return domain.BackupJob{}, ErrConflict
		}
	}

	job := domain.BackupJob{
		ID:        s.newID(),
		AccountID: accountID,
		Mode:      mode,
		Status:    domain.JobQueued,
	}
	if err := s.store.PutBackupJob(ctx, job); err != nil {
		return domain.BackupJob{}, err
	}
	jobCtx, cancel := context.WithCancel(s.root)
	rt := &runtimeJob{
		job:         job,
		transitions: []domain.JobStatus{domain.JobQueued},
		cancel:      cancel,
		done:        make(chan struct{}),
	}
	s.mu.Lock()
	s.jobs[job.ID] = rt
	s.mu.Unlock()
	go s.run(jobCtx, rt, acct)
	return job, nil
}

func (s *Service) Get(ctx context.Context, accountID, jobID string) (domain.BackupJob, error) {
	if rt := s.runtime(jobID); rt != nil {
		job := rt.snapshot()
		if job.AccountID != accountID {
			return domain.BackupJob{}, ErrNotFound
		}
		return job, nil
	}
	job, err := s.store.GetBackupJob(ctx, jobID)
	if err != nil {
		return domain.BackupJob{}, mapStoreErr(err)
	}
	if job.AccountID != accountID {
		return domain.BackupJob{}, ErrNotFound
	}
	return job, nil
}

func (s *Service) Cancel(ctx context.Context, accountID, jobID string) (domain.BackupJob, error) {
	job, err := s.Get(ctx, accountID, jobID)
	if err != nil {
		return domain.BackupJob{}, err
	}
	if job.Status.Terminal() {
		return job, domain.ErrBackupCancelled
	}
	rt := s.runtime(jobID)
	if rt == nil {
		job.Status = domain.JobCancelled
		job.Error = domain.ErrBackupCancelled.Error()
		if err := s.store.PutBackupJob(ctx, job); err != nil {
			return domain.BackupJob{}, err
		}
		return job, nil
	}
	rt.mu.Lock()
	stream := rt.stream
	cancel := rt.cancel
	rt.mu.Unlock()
	if stream != nil {
		_ = stream.Cancel(ctx)
	}
	if cancel != nil {
		cancel()
	}
	select {
	case <-ctx.Done():
		return rt.snapshot(), ctx.Err()
	case <-rt.done:
		got := rt.snapshot()
		if got.Status != domain.JobCancelled {
			return got, domain.ErrBackupCancelled
		}
		return got, nil
	}
}

func (s *Service) Wait(ctx context.Context, jobID string) (domain.BackupJob, error) {
	rt := s.runtime(jobID)
	if rt == nil {
		job, err := s.store.GetBackupJob(ctx, jobID)
		if err != nil {
			return domain.BackupJob{}, mapStoreErr(err)
		}
		return job, nil
	}
	select {
	case <-ctx.Done():
		return rt.snapshot(), ctx.Err()
	case <-rt.done:
		return rt.snapshot(), nil
	}
}

func (s *Service) Transitions(jobID string) []domain.JobStatus {
	rt := s.runtime(jobID)
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]domain.JobStatus(nil), rt.transitions...)
}

func (s *Service) Subscribe(jobID string) (<-chan domain.BackupJob, func()) {
	rt := s.runtime(jobID)
	if rt == nil {
		ch := make(chan domain.BackupJob)
		close(ch)
		return ch, func() {}
	}
	rt.mu.Lock()
	cur := rt.job
	if cur.Status.Terminal() {
		rt.mu.Unlock()
		ch := make(chan domain.BackupJob, 1)
		ch <- cur
		close(ch)
		return ch, func() {}
	}
	ch := make(chan domain.BackupJob, 8)
	ch <- cur
	rt.subscribers = append(rt.subscribers, ch)
	rt.mu.Unlock()
	unsub := func() {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		out := rt.subscribers[:0]
		for _, sub := range rt.subscribers {
			if sub != ch {
				out = append(out, sub)
			}
		}
		rt.subscribers = out
	}
	return ch, unsub
}

func (s *Service) run(ctx context.Context, rt *runtimeJob, acct domain.Account) {
	defer close(rt.done)

	if err := s.setStatus(ctx, rt, domain.JobTransfer); err != nil {
		return
	}

	sess, err := s.sessions(ctx, acct)
	if err != nil {
		s.fail(rt, err)
		return
	}

	req := domain.BackupRequest{AccountID: acct.ID, Mode: rt.snapshot().Mode}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		s.fail(rt, err)
		return
	}
	cursors, err := adb.ListCursors(ctx)
	if err != nil {
		s.fail(rt, err)
		return
	}
	req.Cursors = cursors
	for _, c := range cursors {
		req.TalkerIDs = append(req.TalkerIDs, c.TalkerID)
	}

	stream, err := sess.StartBackup(ctx, req)
	if err != nil {
		s.fail(rt, err)
		return
	}
	rt.mu.Lock()
	rt.stream = stream
	rt.mu.Unlock()

	snap, err := s.transfer(ctx, rt, acct, stream)
	if err != nil {
		s.finishErr(rt, err)
		return
	}
	if err := s.setStatus(ctx, rt, domain.JobOrganize); err != nil {
		return
	}
	if err := s.organize(ctx, rt, acct, snap); err != nil {
		s.finishErr(rt, err)
		return
	}
	if err := s.setStatus(ctx, rt, domain.JobIndex); err != nil {
		return
	}
	// FTS lives in a later PR; this stage exists so clients can show "更新索引".
	if err := s.setStatus(ctx, rt, domain.JobDone); err != nil {
		return
	}
}

func (s *Service) transfer(ctx context.Context, rt *runtimeJob, acct domain.Account, stream domain.BackupStream) (backupfmt.Snapshot, error) {
	snap := backupfmt.Snapshot{AccountID: acct.ID, WxID: acct.WxID, MediaBlobs: map[string][]byte{}}

	var wg sync.WaitGroup
	progress := stream.Progress()
	useProgress := progress != nil
	if useProgress {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ev := range progress {
				rt.mu.Lock()
				if rt.job.Status == domain.JobTransfer {
					if ev.BytesIn > rt.job.BytesIn {
						rt.job.BytesIn = ev.BytesIn
					}
					if ev.SessionsDone > rt.job.SessionsDone {
						rt.job.SessionsDone = ev.SessionsDone
					}
					job := rt.job
					rt.mu.Unlock()
					_ = s.persist(rt, job)
					s.emit(rt, job)
				} else {
					rt.mu.Unlock()
				}
			}
		}()
	}

	if src, ok := stream.(ChunkSource); ok && src.Chunks() != nil {
		for chunk := range src.Chunks() {
			if err := ctx.Err(); err != nil {
				_ = stream.Wait()
				wg.Wait()
				return snap, domain.ErrBackupCancelled
			}
			mergeChunk(&snap, chunk)
			if useProgress {
				continue
			}
			rt.mu.Lock()
			rt.job.SessionsDone++
			if chunk.Bytes > 0 {
				rt.job.BytesIn += chunk.Bytes
			}
			job := rt.job
			rt.mu.Unlock()
			_ = s.persist(rt, job)
			s.emit(rt, job)
		}
	}

	waitErr := stream.Wait()
	wg.Wait()
	if waitErr != nil {
		return snap, waitErr
	}
	if err := ctx.Err(); err != nil {
		return snap, domain.ErrBackupCancelled
	}
	return snap, nil
}

func (s *Service) organize(ctx context.Context, rt *runtimeJob, acct domain.Account, snap backupfmt.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return domain.ErrBackupCancelled
	}
	if s.organizeHold != nil {
		if err := s.organizeHold(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return domain.ErrBackupCancelled
			}
			return err
		}
	}
	pkgDir, err := s.materializePackage(acct, snap)
	if err != nil {
		return err
	}
	if pkgDir != "" {
		got, err := backupfmt.Read(pkgDir)
		if err != nil {
			return err
		}
		snap = got
	}
	if snap.AccountID == "" {
		snap.AccountID = acct.ID
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		return err
	}
	if err := ingestSnapshot(ctx, adb, acct, s.mediaDir(acct), rt.snapshot().Mode, snap, ingestHooks{afterTalker: s.afterTalker}); err != nil {
		return err
	}
	n := countTalkers(snap)
	rt.mu.Lock()
	if n > rt.job.SessionsDone {
		rt.job.SessionsDone = n
	}
	job := rt.job
	rt.mu.Unlock()
	_ = s.persist(rt, job)
	s.emit(rt, job)
	return nil
}

func (s *Service) materializePackage(acct domain.Account, snap backupfmt.Snapshot) (string, error) {
	if len(snap.Messages) > 0 || len(snap.Conversations) > 0 || len(snap.Media) > 0 {
		dir := s.incomingDir(acct)
		if err := backupfmt.Write(dir, snap); err != nil {
			return "", err
		}
		return dir, nil
	}
	if acct.BackupRoot != "" {
		if _, err := os.Stat(filepath.Join(acct.BackupRoot, backupfmt.BackupDBName)); err == nil {
			return acct.BackupRoot, nil
		}
	}
	return "", nil
}

func (s *Service) incomingDir(acct domain.Account) string {
	return filepath.Join(filepath.Dir(sqlite.CanonicalPath(s.dataDir, acct.WxID)), "incoming")
}

func (s *Service) mediaDir(acct domain.Account) string {
	return filepath.Join(filepath.Dir(sqlite.CanonicalPath(s.dataDir, acct.WxID)), "media")
}

func (s *Service) setStatus(ctx context.Context, rt *runtimeJob, st domain.JobStatus) error {
	if err := ctx.Err(); err != nil {
		s.finishErr(rt, domain.ErrBackupCancelled)
		return err
	}
	rt.mu.Lock()
	if rt.job.Status.Terminal() {
		rt.mu.Unlock()
		return errors.New("backup: job already finished")
	}
	rt.job.Status = st
	if st == domain.JobDone {
		rt.job.Error = ""
	}
	rt.transitions = append(rt.transitions, st)
	job := rt.job
	rt.mu.Unlock()
	if err := s.persist(rt, job); err != nil {
		s.fail(rt, err)
		return err
	}
	s.emit(rt, job)
	return nil
}

func (s *Service) finishErr(rt *runtimeJob, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, domain.ErrBackupCancelled) || errors.Is(err, context.Canceled) {
		s.finish(rt, domain.JobCancelled, domain.ErrBackupCancelled)
		return
	}
	s.fail(rt, err)
}

func (s *Service) fail(rt *runtimeJob, err error) {
	s.finish(rt, domain.JobFailed, err)
}

func (s *Service) finish(rt *runtimeJob, st domain.JobStatus, err error) {
	rt.mu.Lock()
	if rt.job.Status.Terminal() {
		rt.mu.Unlock()
		return
	}
	rt.job.Status = st
	if err != nil {
		rt.job.Error = err.Error()
	}
	rt.transitions = append(rt.transitions, st)
	job := rt.job
	rt.mu.Unlock()
	_ = s.persist(rt, job)
	s.emit(rt, job)
}

func (s *Service) persist(rt *runtimeJob, job domain.BackupJob) error {
	return s.store.PutBackupJob(context.Background(), job)
}

func (s *Service) emit(rt *runtimeJob, job domain.BackupJob) {
	terminal := job.Status.Terminal()
	rt.mu.Lock()
	subs := append([]chan domain.BackupJob(nil), rt.subscribers...)
	if terminal {
		rt.subscribers = nil
	}
	rt.mu.Unlock()
	for _, ch := range subs {
		if terminal {
			forceSend(ch, job)
			close(ch)
			continue
		}
		select {
		case ch <- job:
		default:
		}
	}
}

func forceSend(ch chan domain.BackupJob, job domain.BackupJob) {
	for {
		select {
		case ch <- job:
			return
		default:
			select {
			case <-ch:
			default:
				return
			}
		}
	}
}

func (s *Service) runtime(id string) *runtimeJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (rt *runtimeJob) snapshot() domain.BackupJob {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.job
}

func mapStoreErr(err error) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("job-%d", len(b))
	}
	return hex.EncodeToString(b[:])
}

func countTalkers(snap backupfmt.Snapshot) int {
	seen := map[string]struct{}{}
	for _, c := range snap.Conversations {
		if c.TalkerID != "" {
			seen[c.TalkerID] = struct{}{}
		}
	}
	for _, m := range snap.Messages {
		if m.TalkerID != "" {
			seen[m.TalkerID] = struct{}{}
		}
	}
	return len(seen)
}
