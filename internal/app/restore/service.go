package restore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

var (
	ErrNotFound         = errors.New("restore: not found")
	ErrConflict         = errors.New("restore: account already has an active job")
	ErrInvalidSelector  = errors.New("restore: invalid selector")
	ErrNoSession        = errors.New("restore: device session is not configured")
	ErrExportDir        = errors.New("restore: export directory is required")
	ErrUnknownSessionID = errors.New("restore: unknown session_id")
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
}

type runtimeJob struct {
	mu     sync.Mutex
	job    domain.RestoreJob
	cancel context.CancelFunc
	stream domain.RestoreStream
	done   chan struct{}
}

func New(opt Options) (*Service, error) {
	if opt.Store == nil {
		return nil, errors.New("restore: store is required")
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
	jobs, err := s.store.ListRestoreJobs(ctx, "")
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Status.Terminal() {
			continue
		}
		j.Status = domain.JobFailed
		j.Error = "interrupted"
		if err := s.store.PutRestoreJob(ctx, j); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Start(ctx context.Context, accountID string, sel domain.RestoreSelector) (domain.RestoreJob, error) {
	sel, err := normalizeSelector(sel)
	if err != nil {
		return domain.RestoreJob{}, err
	}
	if s.sessions == nil {
		return domain.RestoreJob{}, ErrNoSession
	}
	acct, err := s.store.GetAccount(ctx, accountID)
	if err != nil {
		return domain.RestoreJob{}, mapStoreErr(err)
	}
	if acct.LoginState != domain.LoginStateLoggedIn {
		return domain.RestoreJob{}, domain.ErrNotLoggedIn
	}
	existing, err := s.store.ListRestoreJobs(ctx, accountID)
	if err != nil {
		return domain.RestoreJob{}, err
	}
	for _, j := range existing {
		if !j.Status.Terminal() {
			return domain.RestoreJob{}, ErrConflict
		}
	}

	job := domain.RestoreJob{
		ID:        s.newID(),
		AccountID: accountID,
		Selector:  sel,
		Status:    domain.JobQueued,
	}
	if err := s.store.PutRestoreJob(ctx, job); err != nil {
		return domain.RestoreJob{}, err
	}
	jobCtx, cancel := context.WithCancel(s.root)
	rt := &runtimeJob{
		job:    job,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	s.mu.Lock()
	s.jobs[job.ID] = rt
	s.mu.Unlock()
	go s.run(jobCtx, rt, acct)
	return job, nil
}

func (s *Service) Get(ctx context.Context, accountID, jobID string) (domain.RestoreJob, error) {
	if rt := s.runtime(jobID); rt != nil {
		job := rt.snapshot()
		if job.AccountID != accountID {
			return domain.RestoreJob{}, ErrNotFound
		}
		return job, nil
	}
	job, err := s.store.GetRestoreJob(ctx, jobID)
	if err != nil {
		return domain.RestoreJob{}, mapStoreErr(err)
	}
	if job.AccountID != accountID {
		return domain.RestoreJob{}, ErrNotFound
	}
	return job, nil
}

func (s *Service) Wait(ctx context.Context, jobID string) (domain.RestoreJob, error) {
	rt := s.runtime(jobID)
	if rt == nil {
		job, err := s.store.GetRestoreJob(ctx, jobID)
		if err != nil {
			return domain.RestoreJob{}, mapStoreErr(err)
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

func (s *Service) Export(ctx context.Context, accountID, dir string, sel domain.RestoreSelector) (ExportResult, error) {
	if strings.TrimSpace(dir) == "" {
		return ExportResult{}, ErrExportDir
	}
	sel, err := normalizeSelector(sel)
	if err != nil {
		return ExportResult{}, err
	}
	acct, err := s.store.GetAccount(ctx, accountID)
	if err != nil {
		return ExportResult{}, mapStoreErr(err)
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		return ExportResult{}, err
	}
	snap, err := loadSnapshot(ctx, adb, acct, sel)
	if err != nil {
		return ExportResult{}, err
	}
	if err := backupfmt.Write(dir, snap); err != nil {
		return ExportResult{}, err
	}
	files, err := listPackageFiles(dir)
	if err != nil {
		return ExportResult{}, err
	}
	return ExportResult{Dir: dir, Files: files}, nil
}

func (s *Service) run(ctx context.Context, rt *runtimeJob, acct domain.Account) {
	defer close(rt.done)

	if err := s.setStatus(ctx, rt, domain.JobOrganize); err != nil {
		return
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		s.fail(rt, err)
		return
	}
	snap, err := loadSnapshot(ctx, adb, acct, rt.snapshot().Selector)
	if err != nil {
		s.fail(rt, err)
		return
	}

	sess, err := s.sessions(ctx, acct)
	if err != nil {
		s.fail(rt, err)
		return
	}
	dest := packageDir(s.dataDir, acct, sess)
	if err := backupfmt.Write(dest, snap); err != nil {
		s.fail(rt, err)
		return
	}
	rt.mu.Lock()
	rt.job.SessionsDone = countTalkers(snap)
	job := rt.job
	rt.mu.Unlock()
	_ = s.persist(job)

	if err := s.setStatus(ctx, rt, domain.JobTransfer); err != nil {
		return
	}
	req := domain.RestoreRequest{AccountID: acct.ID, Selector: rt.snapshot().Selector}
	stream, err := sess.StartRestore(ctx, req)
	if err != nil {
		s.fail(rt, err)
		return
	}
	rt.mu.Lock()
	rt.stream = stream
	rt.mu.Unlock()

	if err := s.watch(ctx, rt, stream); err != nil {
		s.finishErr(rt, err)
		return
	}
	if err := s.setStatus(ctx, rt, domain.JobDone); err != nil {
		return
	}
}

func (s *Service) watch(ctx context.Context, rt *runtimeJob, stream domain.RestoreStream) error {
	var wg sync.WaitGroup
	progress := stream.Progress()
	if progress != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ev := range progress {
				rt.mu.Lock()
				if !rt.job.Status.Terminal() && ev.SessionsDone > rt.job.SessionsDone {
					rt.job.SessionsDone = ev.SessionsDone
					job := rt.job
					rt.mu.Unlock()
					_ = s.persist(job)
					continue
				}
				rt.mu.Unlock()
			}
		}()
	}
	waitErr := stream.Wait()
	wg.Wait()
	if waitErr != nil {
		return waitErr
	}
	if err := ctx.Err(); err != nil {
		return domain.ErrBackupCancelled
	}
	return nil
}

func (s *Service) setStatus(ctx context.Context, rt *runtimeJob, st domain.JobStatus) error {
	if err := ctx.Err(); err != nil {
		s.finishErr(rt, domain.ErrBackupCancelled)
		return err
	}
	rt.mu.Lock()
	if rt.job.Status.Terminal() {
		rt.mu.Unlock()
		return errors.New("restore: job already finished")
	}
	rt.job.Status = st
	if st == domain.JobDone {
		rt.job.Error = ""
	}
	job := rt.job
	rt.mu.Unlock()
	if err := s.persist(job); err != nil {
		s.fail(rt, err)
		return err
	}
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
	job := rt.job
	rt.mu.Unlock()
	_ = s.persist(job)
}

func (s *Service) persist(job domain.RestoreJob) error {
	return s.store.PutRestoreJob(context.Background(), job)
}

func (s *Service) runtime(id string) *runtimeJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (rt *runtimeJob) snapshot() domain.RestoreJob {
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

func normalizeSelector(sel domain.RestoreSelector) (domain.RestoreSelector, error) {
	if sel.Kind == "" {
		sel.Kind = domain.RestoreAll
	}
	if !sel.Kind.Valid() {
		return domain.RestoreSelector{}, ErrInvalidSelector
	}
	if sel.Kind == domain.RestoreSessionIDs {
		var ids []string
		seen := map[string]struct{}{}
		for _, id := range sel.SessionIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return domain.RestoreSelector{}, ErrInvalidSelector
		}
		sel.SessionIDs = ids
	} else {
		sel.SessionIDs = nil
	}
	return sel, nil
}

func packageDir(dataDir string, acct domain.Account, sess domain.DeviceSession) string {
	if d, ok := sess.(interface{ RestoreDir() string }); ok {
		if dir := strings.TrimSpace(d.RestoreDir()); dir != "" {
			return dir
		}
	}
	return filepath.Join(filepath.Dir(sqlite.CanonicalPath(dataDir, acct.WxID)), "outgoing")
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
