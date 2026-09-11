package stats

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
	"github.com/wxbackup/wxbackup/testdata/fixtures"
)

func TestAccountFixture(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := seededConn(t)
	got, err := Account(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionCount != 2 || got.MessageCount != 8 {
		t.Fatalf("totals %+v", got)
	}
	want := []TypeCount{
		{MsgType: fixtures.MsgText, Count: 4},
		{MsgType: fixtures.MsgImage, Count: 1},
		{MsgType: fixtures.MsgVoice, Count: 1},
		{MsgType: fixtures.MsgVideo, Count: 1},
		{MsgType: fixtures.MsgSystem, Count: 1},
	}
	if !equalTypes(got.Types, want) {
		t.Fatalf("types %+v want %+v", got.Types, want)
	}
}

func TestAccountEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutAccount(ctx, domain.Account{
		ID: "empty", WxID: "wxid_empty", LoginState: domain.LoginStateLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	adb, err := store.OpenAccount(ctx, "wxid_empty")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Account(ctx, adb.Conn())
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionCount != 0 || got.MessageCount != 0 {
		t.Fatalf("%+v", got)
	}
	if got.Types == nil || len(got.Types) != 0 {
		t.Fatalf("types %+v", got.Types)
	}
}

func TestAccountCountsLiveRowsNotDenormalized(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutAccount(ctx, domain.Account{
		ID: "live", WxID: "wxid_live", LoginState: domain.LoginStateLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	adb, err := store.OpenAccount(ctx, "wxid_live")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC)
	// msg_count is stale on purpose: stats must COUNT rows, not SUM(msg_count).
	if err := adb.PutConversation(ctx, domain.Conversation{
		AccountID: "live", TalkerID: "t1", Kind: domain.ConversationFriend,
		DisplayName: "One", LastMsgTime: base, MsgCount: 99,
	}); err != nil {
		t.Fatal(err)
	}
	if err := adb.PutConversation(ctx, domain.Conversation{
		AccountID: "live", TalkerID: "t2", Kind: domain.ConversationGroup,
		DisplayName: "Two", LastMsgTime: base, MsgCount: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := adb.PutMessage(ctx, domain.Message{
		AccountID: "live", TalkerID: "t1", MsgID: "m1", MsgSeq: 1,
		MsgType: fixtures.MsgText, CreateTime: base, Text: "a",
	}); err != nil {
		t.Fatal(err)
	}
	if err := adb.PutMessage(ctx, domain.Message{
		AccountID: "live", TalkerID: "t1", MsgID: "m2", MsgSeq: 2,
		MsgType: fixtures.MsgImage, CreateTime: base.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := Account(ctx, adb.Conn())
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionCount != 2 || got.MessageCount != 2 {
		t.Fatalf("%+v", got)
	}
	want := []TypeCount{
		{MsgType: fixtures.MsgText, Count: 1},
		{MsgType: fixtures.MsgImage, Count: 1},
	}
	if !equalTypes(got.Types, want) {
		t.Fatalf("types %+v", got.Types)
	}
}

func TestAccountNilDB(t *testing.T) {
	t.Parallel()
	if _, err := Account(context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestAccountIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := fixtures.Seed(ctx, store, dir); err != nil {
		t.Fatal(err)
	}
	if err := fixtures.SeedOther(ctx, store, dir); err != nil {
		t.Fatal(err)
	}
	a1, err := store.OpenAccount(ctx, fixtures.WxID)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.OpenAccount(ctx, fixtures.OtherWxID)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := Account(ctx, a1.Conn())
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Account(ctx, a2.Conn())
	if err != nil {
		t.Fatal(err)
	}
	if s1.SessionCount != 2 || s1.MessageCount != 8 {
		t.Fatalf("a1 %+v", s1)
	}
	if s2.SessionCount != 2 || s2.MessageCount != 4 {
		t.Fatalf("a2 %+v", s2)
	}
	if !equalTypes(s2.Types, []TypeCount{{MsgType: fixtures.MsgText, Count: 4}}) {
		t.Fatalf("a2 types %+v", s2.Types)
	}
}

func seededConn(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := fixtures.Seed(context.Background(), store, dir); err != nil {
		t.Fatal(err)
	}
	adb, err := store.OpenAccount(context.Background(), fixtures.WxID)
	if err != nil {
		t.Fatal(err)
	}
	return adb.Conn()
}

func equalTypes(got, want []TypeCount) bool {
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
