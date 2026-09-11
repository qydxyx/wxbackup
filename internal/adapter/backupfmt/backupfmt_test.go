package backupfmt

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	snap := Snapshot{
		AccountID: "a1",
		WxID:      "wxid_fixture",
		Conversations: []domain.Conversation{
			{
				AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend,
				DisplayName: "Friend", LastMsgTime: now, MsgCount: 2,
			},
			{
				AccountID: "a1", TalkerID: "wxid_room", Kind: domain.ConversationGroup,
				DisplayName: "Room", LastMsgTime: now.Add(time.Minute), MsgCount: 1,
			},
		},
		Messages: []domain.Message{
			{
				AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 1,
				MsgType: 1, CreateTime: now, Text: "hello", Extra: map[string]any{"k": "v"},
			},
			{
				AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m2", MsgSeq: 2,
				MsgType: 1, IsSend: true, CreateTime: now.Add(time.Second), Text: "hi", XML: "<msg/>",
			},
			{
				AccountID: "a1", TalkerID: "wxid_room", MsgID: "m3", MsgSeq: 1,
				MsgType: 3, CreateTime: now.Add(time.Minute), Text: "", XML: "<img md5=\"abc\"/>",
			},
		},
		Media: []domain.MediaObject{
			{AccountID: "a1", MediaID: "md1", Kind: domain.MediaKindImage, SHA256: "abc", Size: 4},
		},
		MediaBlobs: map[string][]byte{"md1": []byte("JPEG")},
	}
	if err := Write(dir, snap); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, BackupDBName)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "BAK_0_TEXT")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "BAK_0_MEDIA")); err != nil {
		t.Fatal(err)
	}

	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "a1" || got.WxID != "wxid_fixture" {
		t.Fatalf("meta %+v", got)
	}
	if len(got.Conversations) != 2 || got.Conversations[0].TalkerID != "wxid_friend" {
		t.Fatalf("sessions %+v", got.Conversations)
	}
	if len(got.Messages) != 3 || got.Messages[0].Text != "hello" || got.Messages[1].IsSend != true {
		t.Fatalf("messages %+v", got.Messages)
	}
	if got.Messages[0].Extra["k"] != "v" {
		t.Fatalf("extra %+v", got.Messages[0].Extra)
	}
	if !got.Messages[0].CreateTime.Equal(now) {
		t.Fatalf("time %v", got.Messages[0].CreateTime)
	}
	if len(got.Media) != 1 || !got.Media[0].Available || !bytes.Equal(got.MediaBlobs["md1"], []byte("JPEG")) {
		t.Fatalf("media %+v blobs=%v", got.Media, got.MediaBlobs)
	}
}

func TestShardRotation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	var msgs []domain.Message
	for i := 0; i < 8; i++ {
		msgs = append(msgs, domain.Message{
			AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m" + string(rune('a'+i)), MsgSeq: int64(i + 1),
			MsgType: 1, CreateTime: now.Add(time.Duration(i) * time.Second),
			Text: "payload-that-forces-rotation-" + string(rune('a'+i)),
		})
	}
	snap := Snapshot{
		AccountID: "a1",
		Conversations: []domain.Conversation{{
			AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend, MsgCount: 8,
		}},
		Messages: msgs,
	}
	if err := WriteWithOptions(dir, snap, Options{MaxShardSize: 64}); err != nil {
		t.Fatal(err)
	}
	var n int
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if len(e.Name()) >= 10 && e.Name()[len(e.Name())-5:] == "_TEXT" {
			n++
		}
	}
	if n < 2 {
		t.Fatalf("expected multiple TEXT shards, got %d files", n)
	}
	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 8 {
		t.Fatalf("got %d messages", len(got.Messages))
	}
}

func TestEncryptedBackupDBNeedsKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// SQLCipher files replace the sqlite header with a salt; we do not guess the key.
	salt := bytes.Repeat([]byte{0x5a}, 4096)
	if err := os.WriteFile(filepath.Join(dir, BackupDBName), salt, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if !errors.Is(err, ErrNeedsKey) {
		t.Fatalf("err=%v", err)
	}
	if len(got.Messages) != 0 || len(got.Conversations) != 0 {
		t.Fatalf("must not invent bodies or sessions from ciphertext: %+v", got)
	}
}

func TestReadableSessionsEncryptedBodies(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeIndexOnly(t, dir, "wxid_friend", "Friend")
	if err := os.WriteFile(filepath.Join(dir, "BAK_0_TEXT"), bytes.Repeat([]byte{0x11}, 64), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if !errors.Is(err, ErrNeedsKey) {
		t.Fatalf("err=%v", err)
	}
	if len(got.Conversations) != 1 || got.Conversations[0].TalkerID != "wxid_friend" {
		t.Fatalf("sessions %+v", got.Conversations)
	}
	if len(got.Messages) != 0 || len(got.MediaBlobs) != 0 {
		t.Fatalf("must not pretend to decrypt: %+v", got)
	}
}

func TestOurFormatBadShardNeedsKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	snap := Snapshot{
		AccountID: "a1",
		Conversations: []domain.Conversation{{
			AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend, MsgCount: 1,
		}},
		Messages: []domain.Message{{
			AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 1,
			MsgType: 1, CreateTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Text: "secret",
		}},
	}
	if err := Write(dir, snap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BAK_0_TEXT"), bytes.Repeat([]byte{0x22}, 128), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if !errors.Is(err, ErrNeedsKey) {
		t.Fatalf("err=%v", err)
	}
	if len(got.Conversations) != 1 {
		t.Fatalf("sessions %+v", got.Conversations)
	}
	for _, m := range got.Messages {
		if m.Text == "secret" {
			t.Fatal("decoded body from unreadable shard")
		}
	}
}

func TestMissingPackage(t *testing.T) {
	t.Parallel()
	if _, err := Read(t.TempDir()); !errors.Is(err, ErrNotPackage) {
		t.Fatalf("err=%v", err)
	}
}

func TestTestdataGeneratedNotCommitted(t *testing.T) {
	dir := filepath.Join("testdata", "generated")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	snap := Snapshot{
		AccountID: "a1",
		Conversations: []domain.Conversation{{
			AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend,
			DisplayName: "Friend", MsgCount: 1,
		}},
		Messages: []domain.Message{{
			AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 1,
			MsgType: 1, CreateTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Text: "fixture",
		}},
	}
	if err := Write(dir, snap); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if err != nil || len(got.Messages) != 1 || got.Messages[0].Text != "fixture" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestImportCanonical(t *testing.T) {
	t.Parallel()
	pkg := t.TempDir()
	now := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	snap := Snapshot{
		AccountID: "a1",
		WxID:      "wxid_fixture",
		Conversations: []domain.Conversation{{
			AccountID: "a1", TalkerID: "wxid_friend", Kind: domain.ConversationFriend,
			DisplayName: "Friend", LastMsgTime: now, MsgCount: 1,
		}},
		Messages: []domain.Message{{
			AccountID: "a1", TalkerID: "wxid_friend", MsgID: "m1", MsgSeq: 7,
			MsgType: 1, CreateTime: now, Text: "from-bak",
		}},
		Media: []domain.MediaObject{{
			AccountID: "a1", MediaID: "md1", Kind: domain.MediaKindFile, SHA256: "ff", Size: 3, Available: true,
		}},
		MediaBlobs: map[string][]byte{"md1": []byte("ABC")},
	}
	if err := Write(pkg, snap); err != nil {
		t.Fatal(err)
	}
	got, err := Read(pkg)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutAccount(ctx, domain.Account{
		ID: "a1", WxID: "wxid_fixture", LoginState: domain.LoginStateLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	acct, err := store.OpenAccount(ctx, "wxid_fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got.Conversations {
		if err := acct.PutConversation(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range got.Messages {
		if err := acct.PutMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range got.Media {
		if err := acct.PutMedia(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	list, err := acct.ListMessages(ctx, "wxid_friend", nil, 10)
	if err != nil || len(list) != 1 || list[0].Text != "from-bak" || list[0].MsgSeq != 7 {
		t.Fatalf("%+v %v", list, err)
	}
	media, err := acct.GetMedia(ctx, "md1")
	if err != nil || !media.Available || media.SHA256 != "ff" {
		t.Fatalf("%+v %v", media, err)
	}
}

func writeIndexOnly(t *testing.T, dir, talker, name string) {
	t.Helper()
	path := filepath.Join(dir, BackupDBName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE Session (
			talker_id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			avatar TEXT NOT NULL DEFAULT '',
			last_msg_time INTEGER NOT NULL DEFAULT 0,
			msg_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE MsgSegments (
			id INTEGER PRIMARY KEY,
			talker_id TEXT NOT NULL,
			file INTEGER NOT NULL,
			offset INTEGER NOT NULL,
			length INTEGER NOT NULL,
			msg_count INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO Session (talker_id, kind, display_name, msg_count) VALUES (?, 'friend', ?, 1)`, talker, name); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO MsgSegments (talker_id, file, offset, length, msg_count) VALUES (?, 0, 0, 32, 1)`, talker); err != nil {
		t.Fatal(err)
	}
}
