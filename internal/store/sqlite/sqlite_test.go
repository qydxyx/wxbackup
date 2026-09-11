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

	acct := mustAccount(t, s, "a1", "wxid_fixture")
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
	acctDir := filepath.Dir(path)
	info, err := os.Stat(acctDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("account dir perm %o", perm)
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
	acct, err := s.OpenAccount(ctx, want.WxID)
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
	acct2, err := s2.OpenAccount(ctx, want.WxID)
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
	if _, err := s.OpenAccount(ctx, "wxid_fixture"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(ctx, "wxid_fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAccount(ctx, "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
	if _, err := os.Stat(CanonicalPath(dir, "wxid_fixture")); !os.IsNotExist(err) {
		t.Fatalf("canonical dir still present: %v", err)
	}
	if _, err := s.OpenAccount(ctx, "wxid_fixture"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("open after delete: %v", err)
	}
}

func TestUniqueTalker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	s := mustOpen(t, dir)
	acct := mustAccount(t, s, "a1", "wxid_fixture")
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
	acct := mustAccount(t, s, "a1", "wxid_fixture")
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
	page1, err := acct.ListMessages(ctx, "wxid_friend", nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || page1[0].MsgID != "m5" || page1[1].MsgID != "m4" {
		t.Fatalf("page1 %+v", ids(page1))
	}
	c1 := CursorFromMessage(page1[len(page1)-1])
	page2, err := acct.ListMessages(ctx, "wxid_friend", &c1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || page2[0].MsgID != "m3" || page2[1].MsgID != "m2" {
		t.Fatalf("page2 %+v", ids(page2))
	}
	c2 := CursorFromMessage(page2[len(page2)-1])
	page3, err := acct.ListMessages(ctx, "wxid_friend", &c2, 2)
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

func TestListMessagesSameCreateTime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	s := mustOpen(t, dir)
	acct := mustAccount(t, s, "a1", "wxid_fixture")
	same := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		m := domain.Message{
			AccountID: "a1", TalkerID: "wxid_room", MsgID: "m" + strconv.Itoa(i),
			MsgSeq: int64(i), MsgType: 1, CreateTime: same, Text: strconv.Itoa(i),
		}
		if err := acct.PutMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	page1, err := acct.ListMessages(ctx, "wxid_room", nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page1); len(got) != 2 || got[0] != "m5" || got[1] != "m4" {
		t.Fatalf("page1 %v", got)
	}
	c := CursorFromMessage(page1[1])
	page2, err := acct.ListMessages(ctx, "wxid_room", &c, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page2); len(got) != 2 || got[0] != "m3" || got[1] != "m2" {
		t.Fatalf("page2 %v", got)
	}
	c = CursorFromMessage(page2[1])
	page3, err := acct.ListMessages(ctx, "wxid_room", &c, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page3); len(got) != 1 || got[0] != "m1" {
		t.Fatalf("page3 %v", got)
	}
}

func TestMessageUpsertAndIsolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	s := mustOpen(t, dir)
	a := mustAccount(t, s, "id-a", "wxid_a")
	b := mustAccount(t, s, "id-b", "wxid_b")
	if CanonicalPath(dir, "wxid_a") == CanonicalPath(dir, "wxid_b") {
		t.Fatal("accounts must not share a canonical path")
	}
	now := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	msg := domain.Message{
		AccountID: "id-a", TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 1,
		MsgType: 1, CreateTime: now, Text: "from-a",
	}
	if err := a.PutMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	msg.Text = "from-a-updated"
	if err := a.PutMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	listA, err := a.ListMessages(ctx, "wxid_friend", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listA) != 1 || listA[0].Text != "from-a-updated" || listA[0].AccountID != "id-a" {
		t.Fatalf("upsert %+v", listA)
	}
	if err := b.PutMessage(ctx, domain.Message{
		AccountID: "id-b", TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 1,
		MsgType: 1, CreateTime: now, Text: "from-b",
	}); err != nil {
		t.Fatal(err)
	}
	listB, err := b.ListMessages(ctx, "wxid_friend", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listB) != 1 || listB[0].Text != "from-b" || listB[0].AccountID != "id-b" {
		t.Fatalf("b %+v", listB)
	}
	if listA[0].Text == listB[0].Text {
		t.Fatal("messages leaked across accounts")
	}
	if err := a.PutMessage(ctx, domain.Message{
		AccountID: "id-b", TalkerID: "wxid_friend", MsgID: "m2", CreateTime: now, Text: "nope",
	}); !errors.Is(err, ErrAccountMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
}

func TestAccountWxIDImmutable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := mustOpen(t, t.TempDir())
	if err := s.PutAccount(ctx, domain.Account{
		ID: "a1", WxID: "wxid_a", LoginState: domain.LoginStateLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutAccount(ctx, domain.Account{
		ID: "a1", WxID: "wxid_renamed", LoginState: domain.LoginStateLoggedIn,
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("wxid change: %v", err)
	}
	if err := s.PutAccount(ctx, domain.Account{
		ID: "a2", WxID: "wxid_a", LoginState: domain.LoginStateLoggedIn,
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("duplicate wxid: %v", err)
	}
	got, err := s.GetAccount(ctx, "a1")
	if err != nil || got.WxID != "wxid_a" {
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

	rj := domain.RestoreJob{
		ID: "r1", AccountID: "a1",
		Selector:     domain.RestoreSelector{Kind: domain.RestoreSessionIDs, SessionIDs: []string{"wxid_friend"}},
		Status:       domain.JobOrganize,
		SessionsDone: 1,
	}
	if err := s.PutRestoreJob(ctx, rj); err != nil {
		t.Fatal(err)
	}
	rj.Status = domain.JobDone
	if err := s.PutRestoreJob(ctx, rj); err != nil {
		t.Fatal(err)
	}
	gotRestore, err := s.GetRestoreJob(ctx, "r1")
	if err != nil || gotRestore.Status != domain.JobDone || gotRestore.Selector.Kind != domain.RestoreSessionIDs {
		t.Fatalf("%+v %v", gotRestore, err)
	}
	if len(gotRestore.Selector.SessionIDs) != 1 || gotRestore.Selector.SessionIDs[0] != "wxid_friend" {
		t.Fatalf("session_ids %+v", gotRestore.Selector.SessionIDs)
	}

	acct, err := s.OpenAccount(ctx, "wxid_fixture")
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
	ctx := context.Background()
	if _, err := s.OpenAccount(ctx, "../escape"); err == nil {
		t.Fatal("expected invalid wxid")
	}
	if _, err := s.OpenAccount(ctx, ""); err == nil {
		t.Fatal("expected empty wxid")
	}
	if _, err := s.OpenAccount(ctx, "wxid_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing account: %v", err)
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

func mustAccount(t *testing.T, s *Store, id, wxid string) *AccountDB {
	t.Helper()
	ctx := context.Background()
	if err := s.PutAccount(ctx, domain.Account{
		ID: id, WxID: wxid, LoginState: domain.LoginStateLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	acct, err := s.OpenAccount(ctx, wxid)
	if err != nil {
		t.Fatal(err)
	}
	return acct
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
