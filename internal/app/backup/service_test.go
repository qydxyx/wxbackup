package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/adapter/devicesession"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

func TestFullJobFromChunks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	fake.Chunks = []Chunk{
		friendChunk(acct.ID, "wxid_friend", now, "hello", 10),
		{
			TalkerID: "wxid_room",
			Conversations: []domain.Conversation{{
				AccountID: acct.ID, TalkerID: "wxid_room", Kind: domain.ConversationGroup,
				DisplayName: "Room", LastMsgTime: now.Add(time.Minute),
			}},
			Messages: []domain.Message{{
				AccountID: acct.ID, TalkerID: "wxid_room", MsgID: "r1", MsgSeq: 1,
				MsgType: 1, CreateTime: now.Add(time.Minute), Text: "room",
			}},
			Bytes: 4,
		},
	}

	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobQueued {
		t.Fatalf("start status %s", job.Status)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != domain.JobDone {
		t.Fatalf("job %+v", done)
	}
	if got := s.Transitions(job.ID); !statusSeq(got, domain.JobQueued, domain.JobTransfer, domain.JobOrganize, domain.JobIndex, domain.JobDone) {
		t.Fatalf("transitions %v", got)
	}

	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := adb.ListMessages(ctx, "wxid_friend", nil, 10)
	if err != nil || len(msgs) != 1 || msgs[0].Text != "hello" {
		t.Fatalf("messages %+v %v", msgs, err)
	}
	cur, err := adb.GetCursor(ctx, "wxid_friend")
	if err != nil || !cur.LastEndTime.Equal(now) || cur.Received < 1 {
		t.Fatalf("cursor %+v %v", cur, err)
	}
	convs, err := adb.ListConversations(ctx)
	if err != nil || len(convs) != 2 {
		t.Fatalf("convs %+v %v", convs, err)
	}
	for _, c := range convs {
		if c.MsgCount != 1 {
			t.Fatalf("msg_count %+v", c)
		}
	}
}

func TestIncrementalSkipsOldMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	t0 := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	fake.Chunks = []Chunk{friendChunk(acct.ID, "wxid_friend", t0, "old", 8)}
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wait(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	fake.Chunks = []Chunk{
		{
			TalkerID: "wxid_friend",
			Messages: []domain.Message{
				{AccountID: acct.ID, TalkerID: "wxid_friend", MsgID: "m-wxid_friend", MsgSeq: 1, MsgType: 1, CreateTime: t0, Text: "old"},
				{AccountID: acct.ID, TalkerID: "wxid_friend", MsgID: "m-new", MsgSeq: 2, MsgType: 1, CreateTime: t0.Add(time.Hour), Text: "new"},
			},
			Bytes: 12,
		},
	}
	job2, err := s.Start(ctx, acct.ID, domain.BackupModeIncremental)
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job2.ID)
	if err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := adb.ListMessages(ctx, "wxid_friend", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %+v", texts(msgs))
	}
	foundNew := false
	for _, m := range msgs {
		if m.Text == "new" {
			foundNew = true
		}
	}
	if !foundNew {
		t.Fatalf("missing new message: %+v", texts(msgs))
	}
	cur, err := adb.GetCursor(ctx, "wxid_friend")
	if err != nil || !cur.LastEndTime.Equal(t0.Add(time.Hour)) || cur.Received != 2 {
		t.Fatalf("cursor %+v %v", cur, err)
	}
	conv, err := adb.GetConversation(ctx, "wxid_friend")
	if err != nil || conv.MsgCount != 2 {
		t.Fatalf("conv %+v %v", conv, err)
	}
	req := fake.LastBackupRequest()
	if len(req.Cursors) != 1 || req.Cursors[0].SegmentMeta == "" {
		t.Fatalf("request cursors %+v", req.Cursors)
	}
}

func TestResumeAfterFailedTransfer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	t0 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	fake.Chunks = []Chunk{
		friendChunk(acct.ID, "wxid_friend", t0, "first", 3),
		friendChunk(acct.ID, "wxid_other", t0.Add(time.Second), "second", 3),
	}
	fake.FailAfter = 1
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobFailed {
		t.Fatalf("%+v %v", done, err)
	}

	fake.FailAfter = 0
	fake.WaitErr = nil
	job2, err := s.Start(ctx, acct.ID, domain.BackupModeResume)
	if err != nil {
		t.Fatal(err)
	}
	done2, err := s.Wait(ctx, job2.ID)
	if err != nil || done2.Status != domain.JobDone {
		t.Fatalf("%+v %v", done2, err)
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := adb.ListMessages(ctx, "wxid_friend", nil, 10)
	if err != nil || len(a) != 1 {
		t.Fatalf("friend %+v %v", a, err)
	}
	b, err := adb.ListMessages(ctx, "wxid_other", nil, 10)
	if err != nil || len(b) != 1 || b[0].Text != "second" {
		t.Fatalf("other %+v %v", b, err)
	}
}

func TestResumeOmitsFinishedTalkers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	t0 := time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC)
	fake.Chunks = []Chunk{
		friendChunk(acct.ID, "wxid_friend", t0, "first", 3),
		friendChunk(acct.ID, "wxid_other", t0.Add(time.Second), "second", 3),
	}
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	if done, err := s.Wait(ctx, job.ID); err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}

	job2, err := s.Start(ctx, acct.ID, domain.BackupModeResume)
	if err != nil {
		t.Fatal(err)
	}
	if done, err := s.Wait(ctx, job2.ID); err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}
	emitted := fake.EmittedTalkers()
	req := fake.LastBackupRequest()
	if len(emitted) != 0 {
		t.Fatalf("finished talkers re-emitted: %v req=%+v", emitted, req.Cursors)
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	for _, talker := range []string{"wxid_friend", "wxid_other"} {
		conv, err := adb.GetConversation(ctx, talker)
		if err != nil || conv.MsgCount != 1 {
			t.Fatalf("%s %+v %v", talker, conv, err)
		}
	}
}

func TestSecondFullDoesNotInflateCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	t0 := time.Date(2024, 3, 3, 0, 0, 0, 0, time.UTC)
	fake.Chunks = []Chunk{friendChunk(acct.ID, "wxid_friend", t0, "hello", 2)}
	for i := 0; i < 2; i++ {
		job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
		if err != nil {
			t.Fatal(err)
		}
		if done, err := s.Wait(ctx, job.ID); err != nil || done.Status != domain.JobDone {
			t.Fatalf("%+v %v", done, err)
		}
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	conv, err := adb.GetConversation(ctx, "wxid_friend")
	if err != nil || conv.MsgCount != 1 {
		t.Fatalf("%+v %v", conv, err)
	}
	n, err := adb.CountMessages(ctx, "wxid_friend")
	if err != nil || n != 1 {
		t.Fatalf("rows %d %v", n, err)
	}
}

func TestPartialOrganizeThenResume(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	t0 := time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC)
	fake.Chunks = []Chunk{
		friendChunk(acct.ID, "wxid_friend", t0, "first", 3),
		friendChunk(acct.ID, "wxid_other", t0.Add(time.Second), "second", 3),
	}
	s.afterTalker = func(talker string) error {
		if talker == "wxid_friend" {
			return errors.New("organize interrupted")
		}
		return nil
	}
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobFailed {
		t.Fatalf("%+v %v", done, err)
	}
	s.afterTalker = nil

	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	friend, err := adb.GetConversation(ctx, "wxid_friend")
	if err != nil || friend.MsgCount != 1 {
		t.Fatalf("friend after partial %+v %v", friend, err)
	}
	if _, err := adb.GetConversation(ctx, "wxid_other"); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("other should be rolled back/absent: %v", err)
	}

	job2, err := s.Start(ctx, acct.ID, domain.BackupModeResume)
	if err != nil {
		t.Fatal(err)
	}
	done2, err := s.Wait(ctx, job2.ID)
	if err != nil || done2.Status != domain.JobDone {
		t.Fatalf("%+v %v", done2, err)
	}
	emitted := fake.EmittedTalkers()
	if len(emitted) != 1 || emitted[0] != "wxid_other" {
		t.Fatalf("resume emitted %v", emitted)
	}
	other, err := adb.GetConversation(ctx, "wxid_other")
	if err != nil || other.MsgCount != 1 {
		t.Fatalf("other %+v %v", other, err)
	}
	friend, err = adb.GetConversation(ctx, "wxid_friend")
	if err != nil || friend.MsgCount != 1 {
		t.Fatalf("friend after resume %+v %v", friend, err)
	}
}

func TestResumeZeroCursorSkipsExistingIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	t0 := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	if err := adb.PutConversation(ctx, domain.Conversation{
		AccountID: acct.ID, TalkerID: "wxid_friend", Kind: domain.ConversationFriend,
		DisplayName: "Friend", LastMsgTime: t0, MsgCount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := adb.PutMessage(ctx, domain.Message{
		AccountID: acct.ID, TalkerID: "wxid_friend", MsgID: "m-wxid_friend", MsgSeq: 1,
		MsgType: 1, CreateTime: t0, Text: "existing",
	}); err != nil {
		t.Fatal(err)
	}
	fake.Chunks = []Chunk{
		friendChunk(acct.ID, "wxid_friend", t0, "existing", 3),
		{
			TalkerID: "wxid_friend",
			Messages: []domain.Message{{
				AccountID: acct.ID, TalkerID: "wxid_friend", MsgID: "m-new", MsgSeq: 2,
				MsgType: 1, CreateTime: t0.Add(time.Minute), Text: "new",
			}},
			Bytes: 2,
		},
	}
	job, err := s.Start(ctx, acct.ID, domain.BackupModeResume)
	if err != nil {
		t.Fatal(err)
	}
	if done, err := s.Wait(ctx, job.ID); err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}
	n, err := adb.CountMessages(ctx, "wxid_friend")
	if err != nil || n != 2 {
		t.Fatalf("rows %d %v", n, err)
	}
	conv, err := adb.GetConversation(ctx, "wxid_friend")
	if err != nil || conv.MsgCount != 2 {
		t.Fatalf("%+v %v", conv, err)
	}
}

func TestCancelDuringOrganize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	fake.Chunks = []Chunk{friendChunk(acct.ID, "wxid_friend", time.Date(2024, 3, 6, 0, 0, 0, 0, time.UTC), "x", 1)}
	s.organizeHold = func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, s, acct.ID, job.ID, domain.JobOrganize)
	got, err := s.Cancel(ctx, acct.ID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobCancelled {
		t.Fatalf("%+v", got)
	}
	if done, err := s.Wait(ctx, job.ID); err != nil || done.Status == domain.JobDone {
		t.Fatalf("must not complete after cancel: %+v %v", done, err)
	}
}

func TestCancelRunningJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	hold, stop := context.WithCancel(context.Background())
	defer stop()
	s, fake, acct := setup(t, nil)
	fake.Hold = hold
	fake.Holding = make(chan struct{})
	fake.Chunks = []Chunk{friendChunk(acct.ID, "wxid_friend", time.Now().UTC(), "x", 1)}
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.Holding:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for transfer hold")
	}
	got, err := s.Cancel(ctx, acct.ID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobCancelled {
		t.Fatalf("%+v", got)
	}
}

func TestIngestBackupfmtPackage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pkg := t.TempDir()
	now := time.Date(2024, 4, 5, 6, 7, 8, 0, time.UTC)
	snap := backupfmt.Snapshot{
		AccountID: "a1",
		WxID:      "wxid_fixture",
		Conversations: []domain.Conversation{{
			AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend,
			DisplayName: "Friend", LastMsgTime: now, MsgCount: 1,
		}},
		Messages: []domain.Message{{
			AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 1,
			MsgType: 1, CreateTime: now, Text: "from-package",
		}},
		Media: []domain.MediaObject{{
			AccountID: "a1", MediaID: "md1", Kind: domain.MediaKindImage, Size: 4,
		}},
		MediaBlobs: map[string][]byte{"md1": []byte("JPEG")},
	}
	if err := backupfmt.Write(pkg, snap); err != nil {
		t.Fatal(err)
	}

	s, fake, acct := setup(t, func(a *domain.Account) { a.BackupRoot = pkg })
	_ = fake
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := adb.ListMessages(ctx, "wxid_friend", nil, 10)
	if err != nil || len(msgs) != 1 || msgs[0].Text != "from-package" {
		t.Fatalf("%+v %v", msgs, err)
	}
	media, err := adb.GetMedia(ctx, "md1")
	if err != nil || !media.Available || media.Path == "" {
		t.Fatalf("%+v %v", media, err)
	}
	body, err := os.ReadFile(media.Path)
	if err != nil || string(body) != "JPEG" {
		t.Fatalf("blob %q %v", body, err)
	}
}

func TestRejectsNotLoggedInAndBadMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutAccount(ctx, domain.Account{
		ID: "a1", WxID: "wxid_fixture", LoginState: domain.LoginStatePending,
	}); err != nil {
		t.Fatal(err)
	}
	fake := &FakeSession{}
	s, err := New(Options{Store: store, DataDir: dir, Sessions: staticSession(fake)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.Start(ctx, "a1", domain.BackupModeFull); !errors.Is(err, domain.ErrNotLoggedIn) {
		t.Fatalf("login: %v", err)
	}
	s2, fake2, acct := setup(t, nil)
	_ = fake2
	if _, err := s2.Start(ctx, acct.ID, domain.BackupMode("delta")); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("mode: %v", err)
	}
}

func TestConflictAndRecoverInterrupted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	hold, stop := context.WithCancel(context.Background())
	defer stop()
	fake.Hold = hold
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx, acct.ID, domain.BackupModeFull); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	stop()
	if _, err := s.Wait(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.store.PutBackupJob(ctx, domain.BackupJob{
		ID: "stuck", AccountID: acct.ID, Mode: domain.BackupModeFull, Status: domain.JobTransfer,
	}); err != nil {
		t.Fatal(err)
	}
	s2, err := New(Options{Store: s.store, DataDir: s.dataDir, Sessions: staticSession(fake)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	got, err := s2.store.GetBackupJob(ctx, "stuck")
	if err != nil || got.Status != domain.JobFailed || got.Error != "interrupted" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSidecarPackageJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	acct := domain.Account{
		ID: "a1", WxID: "wxid_fixture", Nickname: "Fixture",
		LoginState: domain.LoginStateLoggedIn,
	}
	if err := store.PutAccount(ctx, acct); err != nil {
		t.Fatal(err)
	}
	side := t.TempDir()
	now := time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC)
	if err := backupfmt.Write(side, backupfmt.Snapshot{
		AccountID: acct.ID,
		WxID:      acct.WxID,
		Conversations: []domain.Conversation{{
			AccountID: acct.ID, TalkerID: "wxid_friend", Kind: domain.ConversationFriend,
			DisplayName: "Friend", LastMsgTime: now, MsgCount: 1,
		}},
		Messages: []domain.Message{{
			AccountID: acct.ID, TalkerID: "wxid_friend", MsgID: "m-side", MsgSeq: 1,
			MsgType: 1, CreateTime: now, Text: "from-sidecar",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sideSess := devicesession.NewSidecar(side)
	sideSess.PollInterval = 15 * time.Millisecond
	sideSess.StableFor = 20 * time.Millisecond
	s, err := New(Options{Store: store, DataDir: dir, Sessions: func(context.Context, domain.Account) (domain.DeviceSession, error) {
		return sideSess, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := adb.ListMessages(ctx, "wxid_friend", nil, 10)
	if err != nil || len(msgs) != 1 || msgs[0].Text != "from-sidecar" {
		t.Fatalf("messages %+v %v", msgs, err)
	}
}

func TestStartBackupErrorFailsJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t, nil)
	fake.StartErr = domain.ErrDiscoveryPortInUse
	job, err := s.Start(ctx, acct.ID, domain.BackupModeFull)
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobFailed {
		t.Fatalf("%+v %v", done, err)
	}
}

func setup(t *testing.T, tweak func(*domain.Account)) (*Service, *FakeSession, domain.Account) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	acct := domain.Account{
		ID: "a1", WxID: "wxid_fixture", Nickname: "Fixture",
		LoginState: domain.LoginStateLoggedIn,
		BackupRoot: filepath.Join(dir, "backup-root"),
	}
	if tweak != nil {
		tweak(&acct)
	}
	if err := store.PutAccount(ctx, acct); err != nil {
		t.Fatal(err)
	}
	fake := &FakeSession{Account: acct}
	s, err := New(Options{Store: store, DataDir: dir, Sessions: staticSession(fake)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, fake, acct
}

func staticSession(sess domain.DeviceSession) SessionResolver {
	return func(context.Context, domain.Account) (domain.DeviceSession, error) {
		return sess, nil
	}
}

func friendChunk(accountID, talker string, at time.Time, text string, bytes int64) Chunk {
	return Chunk{
		TalkerID: talker,
		Conversations: []domain.Conversation{{
			AccountID: accountID, TalkerID: talker, Kind: domain.ConversationFriend,
			DisplayName: talker, LastMsgTime: at, MsgCount: 1,
		}},
		Messages: []domain.Message{{
			AccountID: accountID, TalkerID: talker, MsgID: "m-" + talker, MsgSeq: 1,
			MsgType: 1, CreateTime: at, Text: text,
		}},
		Bytes: bytes,
	}
}

func statusSeq(got []domain.JobStatus, want ...domain.JobStatus) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func texts(msgs []domain.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Text
	}
	return out
}

func waitStatus(t *testing.T, s *Service, accountID, jobID string, want domain.JobStatus) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := s.Get(ctx, accountID, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == want {
			return
		}
		if job.Status.Terminal() {
			t.Fatalf("reached terminal %s before %s", job.Status, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", want)
}
