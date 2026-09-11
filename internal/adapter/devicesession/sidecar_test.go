package devicesession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/domain"
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
	if len(chunks) != 1 {
		t.Fatalf("chunks %d", len(chunks))
	}
	got := chunks[0]
	if len(got.Messages) != 1 || got.Messages[0].Text != "from-sidecar" {
		t.Fatalf("messages %+v", got.Messages)
	}
	if len(got.Conversations) != 1 || got.Conversations[0].TalkerID != "wxid_friend" {
		t.Fatalf("conversations %+v", got.Conversations)
	}
	if string(got.MediaBlobs["md1"]) != "JPEG" {
		t.Fatalf("media %v", got.MediaBlobs)
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
	if _, err := sess.StartRestore(context.Background(), domain.RestoreRequest{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("restore: %v", err)
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
