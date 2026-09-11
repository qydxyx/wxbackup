package restore

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

func TestRestoreAllWithFakeSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t)
	seedTwoTalkers(t, s.store, acct)

	job, err := s.Start(ctx, acct.ID, domain.RestoreSelector{Kind: domain.RestoreAll})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobQueued {
		t.Fatalf("start %+v", job)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}
	if done.SessionsDone != 2 {
		t.Fatalf("sessions_done %d", done.SessionsDone)
	}
	req := fake.LastRestoreRequest()
	if req.AccountID != acct.ID || req.Selector.Kind != domain.RestoreAll {
		t.Fatalf("request %+v", req)
	}

	out := filepath.Join(filepath.Dir(sqlite.CanonicalPath(s.dataDir, acct.WxID)), "outgoing")
	snap, err := backupfmt.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Conversations) != 2 || len(snap.Messages) != 2 {
		t.Fatalf("package %+v msgs=%d", snap.Conversations, len(snap.Messages))
	}
	if _, err := os.Stat(filepath.Join(out, backupfmt.BackupDBName)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "BAK_0_TEXT")); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSessionIDsFiltersTalkers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t)
	seedTwoTalkers(t, s.store, acct)

	job, err := s.Start(ctx, acct.ID, domain.RestoreSelector{
		Kind: domain.RestoreSessionIDs, SessionIDs: []string{"wxid_friend"},
	})
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}
	if done.SessionsDone != 1 {
		t.Fatalf("sessions_done %d", done.SessionsDone)
	}
	req := fake.LastRestoreRequest()
	if req.Selector.Kind != domain.RestoreSessionIDs || len(req.Selector.SessionIDs) != 1 || req.Selector.SessionIDs[0] != "wxid_friend" {
		t.Fatalf("request %+v", req)
	}
	out := filepath.Join(filepath.Dir(sqlite.CanonicalPath(s.dataDir, acct.WxID)), "outgoing")
	snap, err := backupfmt.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Conversations) != 1 || snap.Conversations[0].TalkerID != "wxid_friend" {
		t.Fatalf("convs %+v", snap.Conversations)
	}
	if len(snap.Messages) != 1 || snap.Messages[0].TalkerID != "wxid_friend" {
		t.Fatalf("msgs %+v", snap.Messages)
	}
}

func TestRestoreUnknownSessionIDFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _, acct := setup(t)
	seedTwoTalkers(t, s.store, acct)
	job, err := s.Start(ctx, acct.ID, domain.RestoreSelector{
		Kind: domain.RestoreSessionIDs, SessionIDs: []string{"wxid_missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobFailed {
		t.Fatalf("%+v %v", done, err)
	}
}

func TestRestoreRequiresSessionAndLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutAccount(ctx, domain.Account{
		ID: "a1", WxID: "wxid_fixture", LoginState: domain.LoginStateLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	bare, err := New(Options{Store: store, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bare.Close() })
	if _, err := bare.Start(ctx, "a1", domain.RestoreSelector{Kind: domain.RestoreAll}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("no session: %v", err)
	}

	s, _, acct := setup(t)
	if err := s.store.PutAccount(ctx, domain.Account{
		ID: acct.ID, WxID: acct.WxID, LoginState: domain.LoginStatePending,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx, acct.ID, domain.RestoreSelector{Kind: domain.RestoreAll}); !errors.Is(err, domain.ErrNotLoggedIn) {
		t.Fatalf("login: %v", err)
	}
	if _, err := s.Start(ctx, acct.ID, domain.RestoreSelector{Kind: domain.RestoreSelectorKind("none")}); !errors.Is(err, ErrInvalidSelector) {
		t.Fatalf("selector: %v", err)
	}
	if _, err := s.Start(ctx, acct.ID, domain.RestoreSelector{Kind: domain.RestoreSessionIDs}); !errors.Is(err, ErrInvalidSelector) {
		t.Fatalf("empty session_ids: %v", err)
	}
}

func TestExportWritesOfficialPackage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _, acct := setup(t)
	seedTwoTalkers(t, s.store, acct)
	dir := t.TempDir()
	got, err := s.Export(ctx, acct.ID, dir, domain.RestoreSelector{Kind: domain.RestoreAll})
	if err != nil {
		t.Fatal(err)
	}
	if got.Dir != dir {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, backupfmt.BackupDBName)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "BAK_0_TEXT")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "BAK_0_MEDIA")); err != nil {
		t.Fatal(err)
	}
	snap, err := backupfmt.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Conversations) != 2 || string(snap.MediaBlobs["md1"]) != "JPEG" {
		t.Fatalf("snap convs=%d media=%v", len(snap.Conversations), snap.MediaBlobs)
	}

	partial := t.TempDir()
	got, err = s.Export(ctx, acct.ID, partial, domain.RestoreSelector{
		Kind: domain.RestoreSessionIDs, SessionIDs: []string{"wxid_room"},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err = backupfmt.Read(got.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Conversations) != 1 || snap.Conversations[0].TalkerID != "wxid_room" {
		t.Fatalf("partial %+v", snap.Conversations)
	}
	if _, err := s.Export(ctx, acct.ID, "", domain.RestoreSelector{}); !errors.Is(err, ErrExportDir) {
		t.Fatalf("empty dir: %v", err)
	}
}

func TestSidecarRestoreWritesPackageThenStarts(t *testing.T) {
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
	seedTwoTalkers(t, store, acct)
	side := t.TempDir()
	sideSess := devicesession.NewSidecar(side)
	s, err := New(Options{Store: store, DataDir: dir, Sessions: func(context.Context, domain.Account) (domain.DeviceSession, error) {
		return sideSess, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	job, err := s.Start(ctx, acct.ID, domain.RestoreSelector{Kind: domain.RestoreAll})
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}
	snap, err := backupfmt.Read(side)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Conversations) != 2 || len(snap.Messages) != 2 {
		t.Fatalf("sidecar package convs=%d msgs=%d", len(snap.Conversations), len(snap.Messages))
	}
	if _, err := os.Stat(filepath.Join(side, "BAK_0_TEXT")); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreConflictAndRecoverInterrupted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t)
	seedTwoTalkers(t, s.store, acct)
	hold, stop := context.WithCancel(context.Background())
	defer stop()
	fake.Hold = hold
	fake.Holding = make(chan struct{})
	job, err := s.Start(ctx, acct.ID, domain.RestoreSelector{Kind: domain.RestoreAll})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.Holding:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restore hold")
	}
	if _, err := s.Start(ctx, acct.ID, domain.RestoreSelector{Kind: domain.RestoreAll}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	got, err := s.Get(ctx, acct.ID, job.ID)
	if err != nil || got.Status != domain.JobTransfer && got.Status != domain.JobOrganize {
		t.Fatalf("get %+v %v", got, err)
	}
	stop()
	if _, err := s.Wait(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.store.PutRestoreJob(ctx, domain.RestoreJob{
		ID: "stuck", AccountID: acct.ID,
		Selector: domain.RestoreSelector{Kind: domain.RestoreAll},
		Status:   domain.JobTransfer,
	}); err != nil {
		t.Fatal(err)
	}
	s2, err := New(Options{Store: s.store, DataDir: s.dataDir, Sessions: staticSession(fake)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	stuck, err := s2.store.GetRestoreJob(ctx, "stuck")
	if err != nil || stuck.Status != domain.JobFailed || stuck.Error != "interrupted" {
		t.Fatalf("%+v %v", stuck, err)
	}
}

func TestStartRestoreErrorFailsJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, fake, acct := setup(t)
	seedTwoTalkers(t, s.store, acct)
	fake.StartErr = domain.ErrDiscoveryPortInUse
	job, err := s.Start(ctx, acct.ID, domain.RestoreSelector{Kind: domain.RestoreAll})
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Wait(ctx, job.ID)
	if err != nil || done.Status != domain.JobFailed {
		t.Fatalf("%+v %v", done, err)
	}
}

func setup(t *testing.T) (*Service, *devicesession.FakeSession, domain.Account) {
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
	}
	if err := store.PutAccount(ctx, acct); err != nil {
		t.Fatal(err)
	}
	fake := &devicesession.FakeSession{Account: acct}
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

func seedTwoTalkers(t *testing.T, store *sqlite.Store, acct domain.Account) {
	t.Helper()
	ctx := context.Background()
	adb, err := store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2024, 9, 1, 12, 0, 0, 0, time.UTC)
	for _, c := range []domain.Conversation{
		{AccountID: acct.ID, TalkerID: "wxid_friend", Kind: domain.ConversationFriend, DisplayName: "Friend", LastMsgTime: now, MsgCount: 1},
		{AccountID: acct.ID, TalkerID: "wxid_room", Kind: domain.ConversationGroup, DisplayName: "Room", LastMsgTime: now.Add(time.Minute), MsgCount: 1},
	} {
		if err := adb.PutConversation(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []domain.Message{
		{AccountID: acct.ID, TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 1, MsgType: 1, CreateTime: now, Text: "hello"},
		{AccountID: acct.ID, TalkerID: "wxid_room", MsgID: "r1", MsgSeq: 1, MsgType: 1, CreateTime: now.Add(time.Minute), Text: "room"},
	} {
		if err := adb.PutMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	blob := filepath.Join(t.TempDir(), "md1.bin")
	if err := os.WriteFile(blob, []byte("JPEG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := adb.PutMedia(ctx, domain.MediaObject{
		AccountID: acct.ID, MediaID: "md1", Kind: domain.MediaKindImage,
		Path: blob, Size: 4, Available: true,
	}); err != nil {
		t.Fatal(err)
	}
}
