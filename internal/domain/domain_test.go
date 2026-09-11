package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEnumValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		ok    bool
		check bool
	}{
		{"login", LoginStateLoggedIn.Valid(), true},
		{"login-bad", LoginState("unknown").Valid(), false},
		{"kind", ConversationFriend.Valid(), true},
		{"kind-bad", ConversationKind("channel").Valid(), false},
		{"mode", BackupModeResume.Valid(), true},
		{"mode-bad", BackupMode("delta").Valid(), false},
		{"status", JobTransfer.Valid(), true},
		{"status-bad", JobStatus("ready").Valid(), false},
		{"restore", RestoreSessionIDs.Valid(), true},
		{"restore-bad", RestoreSelectorKind("none").Valid(), false},
		{"media", MediaKindVoice.Valid(), true},
		{"media-bad", MediaKind("sticker").Valid(), false},
	}
	for _, tc := range cases {
		if tc.ok != tc.check {
			t.Fatalf("%s: got %v want %v", tc.name, tc.ok, tc.check)
		}
	}
	if JobQueued.Terminal() || !JobDone.Terminal() || !JobCancelled.Terminal() {
		t.Fatal("terminal status mapping")
	}
}

func TestWeChatPortsAreFixed(t *testing.T) {
	t.Parallel()
	if WeChatDiscoveryPort != 8011 || WeChatTransferPort != 24011 {
		t.Fatalf("got %d/%d", WeChatDiscoveryPort, WeChatTransferPort)
	}
	if DefaultListenPort != 20365 {
		t.Fatalf("default listen port %d", DefaultListenPort)
	}
}

func TestTypedErrors(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("login: %w", ErrNotLoggedIn)
	if !errors.Is(wrapped, ErrNotLoggedIn) {
		t.Fatal("errors.Is should match by code")
	}
	if errors.Is(ErrNotLoggedIn, ErrBackupCancelled) {
		t.Fatal("distinct codes must not match")
	}
	portErr := DiscoveryPortInUse(8011)
	if !errors.Is(portErr, ErrDiscoveryPortInUse) {
		t.Fatal("port-in-use helper should share code")
	}
	got, ok := AsError(wrapped)
	if !ok || got.Code != CodeNotLoggedIn {
		t.Fatalf("AsError: %+v ok=%v", got, ok)
	}

	sentinels := []*Error{
		ErrNotLoggedIn,
		ErrSameWiFiRequired,
		ErrPhoneNotForeground,
		ErrMediaNeverOpened,
		ErrDiscoveryPortInUse,
		ErrBackupCancelled,
		ErrPasswordRequired,
		ErrPasswordIncorrect,
	}
	seen := map[Code]bool{}
	for _, e := range sentinels {
		if e.Error() == "" {
			t.Fatalf("empty message for %s", e.Code)
		}
		if seen[e.Code] {
			t.Fatalf("duplicate code %s", e.Code)
		}
		seen[e.Code] = true
	}
}

func TestSyntheticFixtureJSON(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	acct := Account{
		ID: "a1", WxID: "wxid_fixture", Nickname: "Fixture",
		LoginState: LoginStateLoggedIn, BackupRoot: "/data/wxid_fixture",
		AccessPwdHash: "not-serialized",
	}
	conv := Conversation{
		AccountID: acct.ID, TalkerID: "wxid_friend", Kind: ConversationFriend,
		DisplayName: "Friend", LastMsgTime: now, MsgCount: 1,
	}
	msg := Message{
		AccountID: acct.ID, TalkerID: conv.TalkerID, MsgID: "m1", MsgSeq: 1,
		MsgType: 1, CreateTime: now, Text: "hello", Extra: map[string]any{"k": "v"},
	}
	media := MediaObject{
		AccountID: acct.ID, MediaID: "md1", Kind: MediaKindImage,
		SHA256: "abc", Path: "media/abc", Size: 12, Available: false,
	}
	job := BackupJob{ID: "j1", AccountID: acct.ID, Mode: BackupModeFull, Status: JobQueued}
	cur := BackupCursor{AccountID: acct.ID, TalkerID: conv.TalkerID, LastEndTime: now, Received: 1, Total: 2}

	raw, err := json.Marshal(acct)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); strings.Contains(got, "not-serialized") {
		t.Fatalf("password hash leaked: %s", got)
	}
	if !acct.HasPassword() {
		t.Fatal("non-empty hash should count as set")
	}
	_ = conv
	_ = msg
	_ = media
	_ = job
	_ = cur
	if !media.Kind.Valid() || media.Available {
		t.Fatal("unopened media stays unavailable")
	}
}

type fakeSession struct{}

func (fakeSession) LoginQR(context.Context) (LoginSession, error) { return LoginSession{ID: "s"}, nil }
func (fakeSession) WaitLoggedIn(context.Context) (Account, error) {
	return Account{ID: "a", LoginState: LoginStateLoggedIn}, nil
}
func (fakeSession) StartBackup(context.Context, BackupRequest) (BackupStream, error) {
	return fakeBackup{}, nil
}
func (fakeSession) StartRestore(context.Context, RestoreRequest) (RestoreStream, error) {
	return fakeRestore{}, nil
}
func (fakeSession) RefreshContacts(context.Context) error { return nil }
func (fakeSession) Close(context.Context) error           { return nil }

type fakeBackup struct{}

func (fakeBackup) Progress() <-chan BackupProgress { return nil }
func (fakeBackup) Wait() error                     { return nil }
func (fakeBackup) Cancel(context.Context) error    { return ErrBackupCancelled }

type fakeRestore struct{}

func (fakeRestore) Progress() <-chan RestoreProgress { return nil }
func (fakeRestore) Wait() error                      { return nil }
func (fakeRestore) Cancel(context.Context) error     { return nil }

var _ DeviceSession = fakeSession{}

func TestDeviceSessionStub(t *testing.T) {
	t.Parallel()
	var s DeviceSession = fakeSession{}
	ctx := context.Background()
	if _, err := s.LoginQR(ctx); err != nil {
		t.Fatal(err)
	}
	acct, err := s.WaitLoggedIn(ctx)
	if err != nil || acct.LoginState != LoginStateLoggedIn {
		t.Fatalf("%+v %v", acct, err)
	}
	stream, err := s.StartBackup(ctx, BackupRequest{AccountID: acct.ID, Mode: BackupModeIncremental})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Cancel(ctx); !errors.Is(err, ErrBackupCancelled) {
		t.Fatalf("cancel: %v", err)
	}
}
