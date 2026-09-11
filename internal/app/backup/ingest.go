package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

func mergeChunk(dst *backupfmt.Snapshot, c Chunk) {
	if dst.MediaBlobs == nil {
		dst.MediaBlobs = map[string][]byte{}
	}
	if c.TalkerID != "" && len(c.Conversations) == 0 && len(c.Messages) > 0 {
		c.Conversations = []domain.Conversation{{
			AccountID: dst.AccountID, TalkerID: c.TalkerID,
			Kind: domain.ConversationFriend, DisplayName: c.TalkerID,
		}}
	}
	dst.Conversations = append(dst.Conversations, c.Conversations...)
	dst.Messages = append(dst.Messages, c.Messages...)
	dst.Media = append(dst.Media, c.Media...)
	for id, blob := range c.MediaBlobs {
		dst.MediaBlobs[id] = blob
	}
}

func ingestSnapshot(ctx context.Context, adb *sqlite.AccountDB, acct domain.Account, mediaDir string, mode domain.BackupMode, snap backupfmt.Snapshot) error {
	if snap.WxID != "" && snap.WxID != acct.WxID {
		return fmt.Errorf("backup: snapshot wxid %q != account %q", snap.WxID, acct.WxID)
	}
	cursors, err := loadCursors(ctx, adb)
	if err != nil {
		return err
	}
	byTalker := map[string][]domain.Message{}
	for _, m := range snap.Messages {
		m.AccountID = acct.ID
		if m.TalkerID == "" || m.MsgID == "" {
			continue
		}
		if skipByCursor(mode, cursors[m.TalkerID], m) {
			continue
		}
		if err := adb.PutMessage(ctx, m); err != nil {
			return err
		}
		byTalker[m.TalkerID] = append(byTalker[m.TalkerID], m)
	}

	convs := dedupeConversations(snap.Conversations)
	for talker, msgs := range byTalker {
		if _, ok := convs[talker]; !ok {
			convs[talker] = domain.Conversation{
				AccountID: acct.ID, TalkerID: talker,
				Kind: domain.ConversationFriend, DisplayName: talker,
			}
		}
		_ = msgs
	}
	for talker, c := range convs {
		c.AccountID = acct.ID
		if !c.Kind.Valid() {
			c.Kind = domain.ConversationFriend
		}
		applied := byTalker[talker]
		last, n := foldMessages(applied)
		existing, err := adb.GetConversation(ctx, talker)
		if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			return err
		}
		if errors.Is(err, sqlite.ErrNotFound) {
			if n == 0 && c.MsgCount == 0 && last.IsZero() {
				// Keep snapshot metadata even when bodies were filtered out.
			}
			if last.After(c.LastMsgTime) {
				c.LastMsgTime = last
			}
			if n > c.MsgCount {
				c.MsgCount = n
			}
			if err := adb.PutConversation(ctx, c); err != nil {
				return err
			}
		} else {
			existing.DisplayName = firstNonEmpty(c.DisplayName, existing.DisplayName)
			existing.Avatar = firstNonEmpty(c.Avatar, existing.Avatar)
			if c.Kind.Valid() {
				existing.Kind = c.Kind
			}
			existing.MsgCount += n
			if last.After(existing.LastMsgTime) {
				existing.LastMsgTime = last
			}
			if err := adb.PutConversation(ctx, existing); err != nil {
				return err
			}
		}
		if err := persistTalkerCursor(ctx, adb, acct.ID, talker, mode, cursors[talker], applied, snap); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return err
	}
	for _, m := range snap.Media {
		m.AccountID = acct.ID
		if blob, ok := snap.MediaBlobs[m.MediaID]; ok && len(blob) > 0 {
			path := filepath.Join(mediaDir, m.MediaID)
			if err := os.WriteFile(path, blob, 0o600); err != nil {
				return err
			}
			m.Path = path
			m.Size = int64(len(blob))
			m.Available = true
		}
		if !m.Kind.Valid() {
			m.Kind = domain.MediaKindOther
		}
		if err := adb.PutMedia(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

func persistTalkerCursor(ctx context.Context, adb *sqlite.AccountDB, accountID, talker string, mode domain.BackupMode, prev domain.BackupCursor, applied []domain.Message, snap backupfmt.Snapshot) error {
	cur := prev
	cur.AccountID = accountID
	cur.TalkerID = talker
	last, n := foldMessages(applied)
	if last.After(cur.LastEndTime) {
		cur.LastEndTime = last
	}
	if n > 0 {
		cur.Received += n
		cur.SegmentMeta = applied[len(applied)-1].MsgID
	}
	var total int64
	for _, m := range snap.Messages {
		if m.TalkerID == talker {
			total++
		}
	}
	if total > cur.Total {
		cur.Total = total
	}
	if mode == domain.BackupModeFull && n > 0 {
		cur.Received = n
		if total == 0 {
			cur.Total = n
		} else {
			cur.Total = total
		}
	}
	if cur.TalkerID == "" {
		return nil
	}
	return adb.PutCursor(ctx, cur)
}

func skipByCursor(mode domain.BackupMode, cur domain.BackupCursor, m domain.Message) bool {
	if mode == domain.BackupModeFull || cur.LastEndTime.IsZero() {
		return false
	}
	return !m.CreateTime.After(cur.LastEndTime)
}

func loadCursors(ctx context.Context, adb *sqlite.AccountDB) (map[string]domain.BackupCursor, error) {
	list, err := adb.ListCursors(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]domain.BackupCursor, len(list))
	for _, c := range list {
		out[c.TalkerID] = c
	}
	return out, nil
}

func dedupeConversations(in []domain.Conversation) map[string]domain.Conversation {
	out := make(map[string]domain.Conversation, len(in))
	for _, c := range in {
		if c.TalkerID == "" {
			continue
		}
		out[c.TalkerID] = c
	}
	return out
}

func foldMessages(msgs []domain.Message) (time.Time, int64) {
	var last time.Time
	for _, m := range msgs {
		if m.CreateTime.After(last) {
			last = m.CreateTime
		}
	}
	return last, int64(len(msgs))
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
