package devicesession

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/domain"
	_ "modernc.org/sqlite"
)

func TestSidecarStartBackupReadsPackage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2024, 9, 1, 12, 0, 0, 0, time.UTC)
	snap := backupfmt.Snapshot{
		AccountID: "a1",
		WxID:      "wxid_fixture",
		Conversations: []domain.Conversation{{
			AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend,
			DisplayName: "Friend", LastMsgTime: now, MsgCount: 1,
		}},
		Messages: []domain.Message{{
			AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 1,
			MsgType: 1, CreateTime: now, Text: "from-sidecar",
		}},
		Media:      []domain.MediaObject{{AccountID: "a1", MediaID: "md1", Kind: domain.MediaKindImage, Size: 4}},
		MediaBlobs: map[string][]byte{"md1": []byte("JPEG")},
	}
	if err := backupfmt.Write(dir, snap); err != nil {
		t.Fatal(err)
	}

	sess := NewSidecar(dir)
	sess.PollInterval = 15 * time.Millisecond
	sess.StableFor = 20 * time.Millisecond
	ctx := context.Background()
	stream, err := sess.StartBackup(ctx, domain.BackupRequest{AccountID: "a1", Mode: domain.BackupModeFull})
	if err != nil {
		t.Fatal(err)
	}
	src, ok := stream.(ChunkSource)
	if !ok {
		t.Fatalf("stream %T is not ChunkSource", stream)
	}
	var chunks []Chunk
	for c := range src.Chunks() {
		chunks = append(chunks, c)
	}
	if err := stream.Wait(); err != nil {
		t.Fatal(err)
	}
	byTalker := map[string]Chunk{}
	var media Chunk
	for _, c := range chunks {
		if c.TalkerID == "" {
			media = c
			continue
		}
		byTalker[c.TalkerID] = c
	}
	got, ok := byTalker["wxid_friend"]
	if !ok || len(got.Messages) != 1 || got.Messages[0].Text != "from-sidecar" {
		t.Fatalf("messages %+v chunks=%d", chunks, len(chunks))
	}
	if len(got.Conversations) != 1 || got.Conversations[0].TalkerID != "wxid_friend" {
		t.Fatalf("conversations %+v", got.Conversations)
	}
	if string(media.MediaBlobs["md1"]) != "JPEG" {
		t.Fatalf("media %v", media.MediaBlobs)
	}

	acct, err := sess.WaitLoggedIn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if acct.WxID != "wxid_fixture" || acct.LoginState != domain.LoginStateLoggedIn || acct.BackupRoot != dir {
		t.Fatalf("account %+v", acct)
	}
}

func TestSidecarStartBackupWaitsForPackage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sess := NewSidecar(dir)
	sess.PollInterval = 15 * time.Millisecond
	sess.StableFor = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	type result struct {
		stream domain.BackupStream
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		st, err := sess.StartBackup(ctx, domain.BackupRequest{Mode: domain.BackupModeFull})
		ch <- result{st, err}
	}()

	select {
	case got := <-ch:
		t.Fatalf("returned before package: %+v", got)
	case <-time.After(40 * time.Millisecond):
	}

	now := time.Date(2024, 9, 2, 0, 0, 0, 0, time.UTC)
	if err := backupfmt.Write(dir, backupfmt.Snapshot{
		AccountID: "a1",
		WxID:      "wxid_late",
		Conversations: []domain.Conversation{{
			AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend, LastMsgTime: now,
		}},
		Messages: []domain.Message{{
			AccountID: "a1", TalkerID: "wxid_friend", MsgID: "late", MsgSeq: 1,
			MsgType: 1, CreateTime: now, Text: "appeared",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	got := <-ch
	if got.err != nil {
		t.Fatal(got.err)
	}
	src := got.stream.(ChunkSource)
	var texts []string
	for c := range src.Chunks() {
		for _, m := range c.Messages {
			texts = append(texts, m.Text)
		}
	}
	if err := got.stream.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 1 || texts[0] != "appeared" {
		t.Fatalf("texts %v", texts)
	}
}

func TestSidecarWaitsForBAKShards(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sess := NewSidecar(dir)
	sess.PollInterval = 15 * time.Millisecond
	sess.StableFor = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type result struct {
		stream domain.BackupStream
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		st, err := sess.StartBackup(ctx, domain.BackupRequest{Mode: domain.BackupModeFull})
		ch <- result{st, err}
	}()

	if err := os.WriteFile(filepath.Join(dir, backupfmt.BackupDBName), []byte("SQLite format 3\x00not-ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		t.Fatalf("Backup.db alone must not complete: %+v", got)
	case <-time.After(80 * time.Millisecond):
	}

	now := time.Date(2024, 9, 3, 0, 0, 0, 0, time.UTC)
	if err := backupfmt.Write(dir, backupfmt.Snapshot{
		AccountID: "a1",
		WxID:      "wxid_late",
		Conversations: []domain.Conversation{{
			AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend, LastMsgTime: now,
		}},
		Messages: []domain.Message{{
			AccountID: "a1", TalkerID: "wxid_friend", MsgID: "late", MsgSeq: 1,
			MsgType: 1, CreateTime: now, Text: "with-shards",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got.err != nil {
		t.Fatal(got.err)
	}
	src := got.stream.(ChunkSource)
	var texts []string
	for c := range src.Chunks() {
		for _, m := range c.Messages {
			texts = append(texts, m.Text)
		}
	}
	if err := got.stream.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 1 || texts[0] != "with-shards" {
		t.Fatalf("texts %v", texts)
	}
}

func TestSidecarEncryptedBackupFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, backupfmt.BackupDBName), []byte("SQLCipher\x00not-sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BAK_0_TEXT"), []byte("encrypted-shard"), 0o600); err != nil {
		t.Fatal(err)
	}
	sess := NewSidecar(dir)
	sess.PollInterval = 15 * time.Millisecond
	sess.StableFor = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := sess.StartBackup(ctx, domain.BackupRequest{Mode: domain.BackupModeFull})
	if stream != nil {
		t.Fatalf("stream %T", stream)
	}
	if !errors.Is(err, backupfmt.ErrNeedsKey) {
		t.Fatalf("err %v", err)
	}
}

func TestSidecarGarbageIndexFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, backupfmt.BackupDBName)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE not_a_backup (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BAK_0_TEXT"), []byte("WXT1junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	sess := NewSidecar(dir)
	sess.PollInterval = 15 * time.Millisecond
	sess.StableFor = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := sess.StartBackup(ctx, domain.BackupRequest{Mode: domain.BackupModeFull})
	if stream != nil {
		t.Fatalf("stream %T", stream)
	}
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	if !errors.Is(err, backupfmt.ErrNotPackage) && !errors.Is(err, backupfmt.ErrNeedsKey) {
		t.Fatalf("want package/key error, got %v", err)
	}
}

func TestSidecarIncrementalTwoTalkers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	t0 := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	if err := backupfmt.Write(dir, twoTalkerSnap("a1", t0, t1, "old-a", "old-b", "new-a")); err != nil {
		t.Fatal(err)
	}
	sess := NewSidecar(dir)
	sess.PollInterval = 15 * time.Millisecond
	sess.StableFor = 20 * time.Millisecond
	ctx := context.Background()
	stream, err := sess.StartBackup(ctx, domain.BackupRequest{
		AccountID: "a1",
		Mode:      domain.BackupModeIncremental,
		Cursors: []domain.BackupCursor{
			{TalkerID: "wxid_a", LastEndTime: t0, SegmentMeta: "m-a-old"},
			{TalkerID: "wxid_b", LastEndTime: t0, SegmentMeta: "m-b-old"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	src := stream.(ChunkSource)
	got := map[string][]string{}
	for c := range src.Chunks() {
		for _, m := range c.Messages {
			got[c.TalkerID] = append(got[c.TalkerID], m.Text)
		}
	}
	if err := stream.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(got["wxid_a"]) != 1 || got["wxid_a"][0] != "new-a" {
		t.Fatalf("talker a %v", got["wxid_a"])
	}
	if _, ok := got["wxid_b"]; ok {
		t.Fatalf("talker b should be omitted: %v", got["wxid_b"])
	}
}

func twoTalkerSnap(accountID string, tOld, tNew time.Time, oldA, oldB, newA string) backupfmt.Snapshot {
	return backupfmt.Snapshot{
		AccountID: accountID,
		WxID:      "wxid_fixture",
		Conversations: []domain.Conversation{
			{AccountID: accountID, TalkerID: "wxid_a", Kind: domain.ConversationFriend, DisplayName: "A", LastMsgTime: tNew, MsgCount: 2},
			{AccountID: accountID, TalkerID: "wxid_b", Kind: domain.ConversationFriend, DisplayName: "B", LastMsgTime: tOld, MsgCount: 1},
		},
		Messages: []domain.Message{
			{AccountID: accountID, TalkerID: "wxid_a", MsgID: "m-a-old", MsgSeq: 1, MsgType: 1, CreateTime: tOld, Text: oldA},
			{AccountID: accountID, TalkerID: "wxid_a", MsgID: "m-a-new", MsgSeq: 2, MsgType: 1, CreateTime: tNew, Text: newA},
			{AccountID: accountID, TalkerID: "wxid_b", MsgID: "m-b-old", MsgSeq: 1, MsgType: 1, CreateTime: tOld, Text: oldB},
		},
	}
}

func TestSidecarStartBackupCancel(t *testing.T) {
	t.Parallel()
	sess := NewSidecar(t.TempDir())
	sess.PollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := sess.StartBackup(ctx, domain.BackupRequest{})
	if !errors.Is(err, domain.ErrBackupCancelled) && !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestSidecarRejectsFilePath(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewSidecar(path).StartBackup(context.Background(), domain.BackupRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolver(t *testing.T) {
	t.Parallel()
	if Resolver("") != nil || Resolver("  ") != nil {
		t.Fatal("empty dir must yield nil resolver")
	}
	dir := t.TempDir()
	r := Resolver(dir)
	if r == nil {
		t.Fatal("expected resolver")
	}
	sess, err := r(context.Background(), domain.Account{})
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := sess.(*Sidecar)
	if !ok || sc.Dir != dir {
		t.Fatalf("%T %+v", sess, sess)
	}
	if _, err := sess.StartRestore(context.Background(), domain.RestoreRequest{}); err == nil {
		t.Fatal("empty sidecar package must fail restore")
	}
}

func TestSidecarStartRestoreReadsPackage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2024, 9, 3, 0, 0, 0, 0, time.UTC)
	if err := backupfmt.Write(dir, backupfmt.Snapshot{
		AccountID: "a1",
		WxID:      "wxid_fixture",
		Conversations: []domain.Conversation{
			{AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend, LastMsgTime: now, MsgCount: 1},
			{AccountID: "a1", TalkerID: "wxid_room", Kind: domain.ConversationGroup, LastMsgTime: now, MsgCount: 1},
		},
		Messages: []domain.Message{
			{AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 1, MsgType: 1, CreateTime: now, Text: "hi"},
			{AccountID: "a1", TalkerID: "wxid_room", MsgID: "r1", MsgSeq: 1, MsgType: 1, CreateTime: now, Text: "room"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	sess := NewSidecar(dir)
	stream, err := sess.StartRestore(context.Background(), domain.RestoreRequest{
		AccountID: "a1",
		Selector:  domain.RestoreSelector{Kind: domain.RestoreSessionIDs, SessionIDs: []string{"wxid_friend"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var last int
	for ev := range stream.Progress() {
		last = ev.SessionsDone
	}
	if err := stream.Wait(); err != nil {
		t.Fatal(err)
	}
	if last != 1 {
		t.Fatalf("sessions_done %d", last)
	}
}

func TestFakeSessionStartRestore(t *testing.T) {
	t.Parallel()
	fake := &FakeSession{RestoreTalkers: []string{"wxid_a", "wxid_b"}}
	stream, err := fake.StartRestore(context.Background(), domain.RestoreRequest{
		Selector: domain.RestoreSelector{Kind: domain.RestoreAll},
	})
	if err != nil {
		t.Fatal(err)
	}
	var last int
	for ev := range stream.Progress() {
		last = ev.SessionsDone
	}
	if err := stream.Wait(); err != nil {
		t.Fatal(err)
	}
	if last != 2 {
		t.Fatalf("sessions_done %d", last)
	}
	req := fake.LastRestoreRequest()
	if req.Selector.Kind != domain.RestoreAll {
		t.Fatalf("%+v", req)
	}
}

func TestFakeSessionImplementsDeviceSession(t *testing.T) {
	t.Parallel()
	var sess domain.DeviceSession = &FakeSession{Account: domain.Account{WxID: "wxid_x"}}
	acct, err := sess.WaitLoggedIn(context.Background())
	if err != nil || acct.WxID != "wxid_x" {
		t.Fatalf("%+v %v", acct, err)
	}
	ls, err := sess.LoginQR(context.Background())
	if err != nil || ls.ID != "fake" {
		t.Fatalf("%+v %v", ls, err)
	}
}
