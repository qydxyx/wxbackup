// Package fixtures seeds a synthetic account for viewer tests.
// Names, ids, and bytes are invented; this is not a WeChat capture.
package fixtures

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

const (
	AccountID        = "a1"
	WxID             = "wxid_fixture"
	Nickname         = "Fixture User"
	OtherAccountID   = "a2"
	OtherWxID        = "wxid_other"
	OtherNickname    = "Other Fixture"
	FriendTalkerID   = "wxid_friend"
	GroupTalkerID    = "wxid_group"
	FriendName       = "Synthetic Friend"
	GroupName        = "Synthetic Group"
	OtherFriendName  = "Other Friend"
	OtherGroupName   = "Other Group"
	AvailableMediaID = "media_available"
	MissingMediaID   = "media_missing"

	// Synthetic type tags for the viewer; not a protocol dump.
	MsgText   = 1
	MsgImage  = 3
	MsgVoice  = 34
	MsgVideo  = 43
	MsgSystem = 10000
)

var AvailableMediaBytes = []byte("synthetic-image-bytes")

// Seed writes one account, friend+group conversations, mixed message types,
// one on-disk media object, and one never-opened media id.
func Seed(ctx context.Context, store *sqlite.Store, dataDir string) error {
	if store == nil {
		return fmt.Errorf("fixtures: store is required")
	}
	acct := domain.Account{
		ID:            AccountID,
		WxID:          WxID,
		Nickname:      Nickname,
		Avatar:        "avatar/fixture.png",
		LoginState:    domain.LoginStateLoggedIn,
		BackupRoot:    filepath.Join(dataDir, WxID),
		AccessPwdHash: "not-serialized",
	}
	if err := store.PutAccount(ctx, acct); err != nil {
		return err
	}
	adb, err := store.OpenAccount(ctx, WxID)
	if err != nil {
		return err
	}

	base := time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC)
	friendLast := base.Add(6 * time.Second)
	groupLast := base.Add(2 * time.Second)

	if err := adb.PutConversation(ctx, domain.Conversation{
		AccountID: AccountID, TalkerID: FriendTalkerID, Kind: domain.ConversationFriend,
		DisplayName: FriendName, LastMsgTime: friendLast, MsgCount: 6,
	}); err != nil {
		return err
	}
	if err := adb.PutConversation(ctx, domain.Conversation{
		AccountID: AccountID, TalkerID: GroupTalkerID, Kind: domain.ConversationGroup,
		DisplayName: GroupName, LastMsgTime: groupLast, MsgCount: 2,
	}); err != nil {
		return err
	}

	mediaDir := filepath.Join(dataDir, "accounts", WxID, "media")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return err
	}
	availPath := filepath.Join(mediaDir, "available.bin")
	if err := os.WriteFile(availPath, AvailableMediaBytes, 0o600); err != nil {
		return err
	}
	if err := adb.PutMedia(ctx, domain.MediaObject{
		AccountID: AccountID, MediaID: AvailableMediaID, Kind: domain.MediaKindImage,
		SHA256: "synthetic-sha256", Path: availPath, Size: int64(len(AvailableMediaBytes)), Available: true,
	}); err != nil {
		return err
	}
	if err := adb.PutMedia(ctx, domain.MediaObject{
		AccountID: AccountID, MediaID: MissingMediaID, Kind: domain.MediaKindVoice,
		Path: "media/never-opened", Available: false,
	}); err != nil {
		return err
	}

	friendMsgs := []domain.Message{
		{AccountID: AccountID, TalkerID: FriendTalkerID, MsgID: "m1", MsgSeq: 1, MsgType: MsgText, CreateTime: base.Add(1 * time.Second), Text: "hello fixture"},
		{AccountID: AccountID, TalkerID: FriendTalkerID, MsgID: "m2", MsgSeq: 2, MsgType: MsgImage, IsSend: true, CreateTime: base.Add(2 * time.Second), Extra: map[string]any{"media_id": AvailableMediaID}},
		{AccountID: AccountID, TalkerID: FriendTalkerID, MsgID: "m3", MsgSeq: 3, MsgType: MsgVoice, CreateTime: base.Add(3 * time.Second), Extra: map[string]any{"media_id": MissingMediaID}},
		{AccountID: AccountID, TalkerID: FriendTalkerID, MsgID: "m4", MsgSeq: 4, MsgType: MsgVideo, CreateTime: base.Add(4 * time.Second), Text: "video-placeholder"},
		{AccountID: AccountID, TalkerID: FriendTalkerID, MsgID: "m5", MsgSeq: 5, MsgType: MsgText, IsSend: true, CreateTime: base.Add(5 * time.Second), Text: "later text"},
		{AccountID: AccountID, TalkerID: FriendTalkerID, MsgID: "m6", MsgSeq: 6, MsgType: MsgSystem, CreateTime: friendLast, Text: "synthetic system notice"},
	}
	for _, m := range friendMsgs {
		if err := adb.PutMessage(ctx, m); err != nil {
			return err
		}
	}
	groupMsgs := []domain.Message{
		{AccountID: AccountID, TalkerID: GroupTalkerID, MsgID: "g1", MsgSeq: 1, MsgType: MsgText, CreateTime: base.Add(1 * time.Second), Text: "group hello"},
		{AccountID: AccountID, TalkerID: GroupTalkerID, MsgID: "g2", MsgSeq: 2, MsgType: MsgText, CreateTime: groupLast, Text: "group later"},
	}
	for _, m := range groupMsgs {
		if err := adb.PutMessage(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// SeedOther writes a second account that reuses talker/media ids with
// different payloads so viewer tests can prove account_id isolation.
func SeedOther(ctx context.Context, store *sqlite.Store, dataDir string) error {
	if store == nil {
		return fmt.Errorf("fixtures: store is required")
	}
	acct := domain.Account{
		ID:            OtherAccountID,
		WxID:          OtherWxID,
		Nickname:      OtherNickname,
		LoginState:    domain.LoginStateLoggedIn,
		BackupRoot:    filepath.Join(dataDir, OtherWxID),
		AccessPwdHash: "other-not-serialized",
	}
	if err := store.PutAccount(ctx, acct); err != nil {
		return err
	}
	adb, err := store.OpenAccount(ctx, OtherWxID)
	if err != nil {
		return err
	}
	base := time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC)
	friendLast := base.Add(6 * time.Second)
	if err := adb.PutConversation(ctx, domain.Conversation{
		AccountID: OtherAccountID, TalkerID: FriendTalkerID, Kind: domain.ConversationFriend,
		DisplayName: OtherFriendName, LastMsgTime: friendLast, MsgCount: 2,
	}); err != nil {
		return err
	}
	if err := adb.PutConversation(ctx, domain.Conversation{
		AccountID: OtherAccountID, TalkerID: GroupTalkerID, Kind: domain.ConversationGroup,
		DisplayName: OtherGroupName, LastMsgTime: base.Add(2 * time.Second), MsgCount: 1,
	}); err != nil {
		return err
	}
	// Same media_id as a1 but never opened: GET must 404, not a1's bytes.
	if err := adb.PutMedia(ctx, domain.MediaObject{
		AccountID: OtherAccountID, MediaID: AvailableMediaID, Kind: domain.MediaKindImage,
		Available: false,
	}); err != nil {
		return err
	}
	msgs := []domain.Message{
		{AccountID: OtherAccountID, TalkerID: FriendTalkerID, MsgID: "m1", MsgSeq: 1, MsgType: MsgText, CreateTime: base.Add(1 * time.Second), Text: "other-hello"},
		{AccountID: OtherAccountID, TalkerID: FriendTalkerID, MsgID: "m5", MsgSeq: 5, MsgType: MsgText, CreateTime: base.Add(5 * time.Second), Text: "other-later"},
		{AccountID: OtherAccountID, TalkerID: FriendTalkerID, MsgID: "m6", MsgSeq: 6, MsgType: MsgText, CreateTime: friendLast, Text: "other-system"},
		{AccountID: OtherAccountID, TalkerID: GroupTalkerID, MsgID: "g1", MsgSeq: 1, MsgType: MsgText, CreateTime: base.Add(1 * time.Second), Text: "other-group"},
	}
	for _, m := range msgs {
		if err := adb.PutMessage(ctx, m); err != nil {
			return err
		}
	}
	return nil
}
