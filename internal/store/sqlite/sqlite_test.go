package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
)

func TestWALAndLayout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := mustOpen(t, dir)
	if _, err := os.Stat(MetaPath(dir)); err != nil {
		t.Fatal(err)
	}
	if got := journalMode(t, s.meta); !strings.EqualFold(got, "wal") {
		t.Fatalf("meta journal_mode=%s", got)
	}

	acct, err := s.OpenAccount("wxid_fixture")
	if err != nil {
		t.Fatal(err)
	}
	path := CanonicalPath(dir, "wxid_fixture")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if got := journalMode(t, acct.db); !strings.EqualFold(got, "wal") {
		t.Fatalf("canonical journal_mode=%s", got)
	}
	if !strings.HasSuffix(filepath.ToSlash(path), "accounts/wxid_fixture/canonical.db") {
		t.Fatalf("path %s", path)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	s := mustOpen(t, dir)
	want := domain.Account{
		ID: "a1", WxID: "wxid_fixture", Nickname: "Fixture",
		LoginState: domain.LoginStateLoggedIn, BackupRoot: filepath.Join(dir, "wxid_fixture"),
		AccessPwdHash: "hash-not-plain",
	}
	if err := s.PutAccount(ctx, want); err != nil {
		t.Fatal(err)
	}
	v1, err := schemaVersion(s.meta)
	if err != nil || v1 != len(metaMigrations) {
		t.Fatalf("version %d err=%v", v1, err)
	}
	acct, err := s.OpenAccount(want.WxID)
	if err != nil {
		t.Fatal(err)
	}
	av1, err := schemaVersion(acct.db)
	if err != nil || av1 != len(accountMigrations) {
		t.Fatalf("account version %d err=%v", av1, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := mustOpen(t, dir)
	v2, err := schemaVersion(s2.meta)
	if err != nil || v2 != v1 {
		t.Fatalf("reopen version %d want %d err=%v", v2, v1, err)
	}
	got, err := s2.GetAccountByWxID(ctx, want.WxID)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	acct2, err := s2.OpenAccount(want.WxID)
	if err != nil {
		t.Fatal(err)
	}
	av2, err := schemaVersion(acct2.db)
	if err != nil || av2 != av1 {
		t.Fatalf("reopen account version %d want %d err=%v", av2, av1, err)
	}
}

func TestAccountCRUD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	s := mustOpen(t, dir)
	a := domain.Account{
		ID: "a1", WxID: "wxid_fixture", Nickname: "One",
		LoginState: domain.LoginStatePending, AccessPwdHash: "h1",
	}
	if err := s.PutAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.Nickname = "Two"
	a.LoginState = domain.LoginStateLoggedIn
	if err := s.PutAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAccount(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Nickname != "Two" || got.AccessPwdHash != "h1" {
		t.Fatalf("%+v", got)
	}
	list, err := s.ListAccounts(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	if err := s.DeleteAccount(ctx, "wxid_fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAccount(ctx, "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}

func TestUniqueTalker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	s := mustOpen(t, dir)
	acct, err := s.OpenAccount("wxid_fixture")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	c := domain.Conversation{
		AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend,
		DisplayName: "Friend", LastMsgTime: now, MsgCount: 1,
	}
	if err := acct.PutConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	c.DisplayName = "Renamed"
	c.MsgCount = 2
	if err := acct.PutConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	list, err := acct.ListConversations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("unique talker: got %d rows", len(list))
	}
	if list[0].DisplayName != "Renamed" || list[0].MsgCount != 2 {
		t.Fatalf("%+v", list[0])
	}
	got, err := acct.GetConversation(ctx, "wxid_friend")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastMsgTime.Equal(now) {
		t.Fatalf("time %v", got.LastMsgTime)
	}
}

func TestInsertListPageMessages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	s := mustOpen(t, dir)
	acct, err := s.OpenAccount("wxid_fixture")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		m := domain.Message{
			AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m" + strconv.Itoa(i), MsgSeq: int64(i),
			MsgType: 1, IsSend: i%2 == 0, CreateTime: base.Add(time.Duration(i) * time.Second),
			Text: "hello-" + strconv.Itoa(i), Extra: map[string]any{"n": float64(i)},
		}
		if err := acct.PutMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	page1, err := acct.ListMessages(ctx, "wxid_friend", time.Time{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || page1[0].MsgID != "m5" || page1[1].MsgID != "m4" {
		t.Fatalf("page1 %+v", ids(page1))
	}
	page2, err := acct.ListMessages(ctx, "wxid_friend", page1[len(page1)-1].CreateTime, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || page2[0].MsgID != "m3" || page2[1].MsgID != "m2" {
		t.Fatalf("page2 %+v", ids(page2))
	}
	page3, err := acct.ListMessages(ctx, "wxid_friend", page2[len(page2)-1].CreateTime, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 1 || page3[0].MsgID != "m1" || page3[0].Extra["n"] != float64(1) {
		t.Fatalf("page3 %+v extra=%v", ids(page3), page3[0].Extra)
	}
	got, err := acct.GetMessage(ctx, "wxid_friend", "m1")
	if err != nil || got.Text != "hello-1" || got.IsSend {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestMediaCursorJob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	s := mustOpen(t, dir)
	if err := s.PutAccount(ctx, domain.Account{
		ID: "a1", WxID: "wxid_fixture", LoginState: domain.LoginStateLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	job := domain.BackupJob{
		ID: "j1", AccountID: "a1", Mode: domain.BackupModeIncremental,
		Status: domain.JobTransfer, BytesIn: 9, SessionsDone: 2,
	}
	if err := s.PutBackupJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	job.Status = domain.JobDone
	if err := s.PutBackupJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	gotJob, err := s.GetBackupJob(ctx, "j1")
	if err != nil || gotJob.Status != domain.JobDone || gotJob.BytesIn != 9 {
		t.Fatalf("%+v %v", gotJob, err)
	}
	jobs, err := s.ListBackupJobs(ctx, "a1")
	if err != nil || len(jobs) != 1 {
		t.Fatalf("%+v %v", jobs, err)
	}

	acct, err := s.OpenAccount("wxid_fixture")
	if err != nil {
		t.Fatal(err)
	}
	media := domain.MediaObject{
		AccountID: "a1", MediaID: "md1", Kind: domain.MediaKindImage,
		SHA256: "abc", Path: "media/abc", Size: 12, Available: false,
	}
	if err := acct.PutMedia(ctx, media); err != nil {
		t.Fatal(err)
	}
	gotMedia, err := acct.GetMedia(ctx, "md1")
	if err != nil || gotMedia.Available || gotMedia.Size != 12 {
		t.Fatalf("%+v %v", gotMedia, err)
	}
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	cur := domain.BackupCursor{
		AccountID: "a1", TalkerID: "wxid_friend", LastEndTime: now,
		SegmentMeta: "seg-1", Received: 1, Total: 2,
	}
	if err := acct.PutCursor(ctx, cur); err != nil {
		t.Fatal(err)
	}
	gotCur, err := acct.GetCursor(ctx, "wxid_friend")
	if err != nil || gotCur.SegmentMeta != "seg-1" || !gotCur.LastEndTime.Equal(now) {
		t.Fatalf("%+v %v", gotCur, err)
	}
}

func TestRejectBadWxID(t *testing.T) {
	t.Parallel()
	s := mustOpen(t, t.TempDir())
	if _, err := s.OpenAccount("../escape"); err == nil {
		t.Fatal("expected invalid wxid")
	}
	if _, err := s.OpenAccount(""); err == nil {
		t.Fatal("expected empty wxid")
	}
}

func mustOpen(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func journalMode(t *testing.T, db *sql.DB) string {
	t.Helper()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	return mode
}

func ids(msgs []domain.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.MsgID
	}
	return out
}
