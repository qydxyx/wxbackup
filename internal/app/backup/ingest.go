package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

func mergeChunk(dst *backupfmt.Snapshot, c Chunk) {
	if dst.MediaBlobs == nil {
		dst.MediaBlobs = map[string][]byte{}
	}
	if c.TalkerID != "" && len(c.Conversations) == 0 && len(c.Messages) > 0 && !hasTalker(dst.Conversations, c.TalkerID) {
		c.Conversations = []domain.Conversation{{
			AccountID: dst.AccountID, TalkerID: c.TalkerID,
			Kind: domain.ConversationFriend, DisplayName: c.TalkerID,
		}}
	}
	for _, conv := range c.Conversations {
		if hasTalker(dst.Conversations, conv.TalkerID) {
			continue
		}
		dst.Conversations = append(dst.Conversations, conv)
	}
	dst.Messages = append(dst.Messages, c.Messages...)
	dst.Media = append(dst.Media, c.Media...)
	for id, blob := range c.MediaBlobs {
		dst.MediaBlobs[id] = blob
	}
}

type ingestHooks struct {
	afterTalker func(talker string) error
}

func ingestSnapshot(ctx context.Context, adb *sqlite.AccountDB, acct domain.Account, mediaDir string, mode domain.BackupMode, snap backupfmt.Snapshot, hooks ingestHooks) error {
	if err := ctx.Err(); err != nil {
		return domain.ErrBackupCancelled
	}
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
		byTalker[m.TalkerID] = append(byTalker[m.TalkerID], m)
	}

	convs := dedupeConversations(snap.Conversations)
	for talker := range byTalker {
		if _, ok := convs[talker]; !ok {
			convs[talker] = domain.Conversation{
				AccountID: acct.ID, TalkerID: talker,
				Kind: domain.ConversationFriend, DisplayName: talker,
			}
		}
	}
	talkers := make([]string, 0, len(convs))
	for talker := range convs {
		talkers = append(talkers, talker)
	}
	sort.Strings(talkers)

	for _, talker := range talkers {
		if err := ctx.Err(); err != nil {
			return domain.ErrBackupCancelled
		}
		c := convs[talker]
		c.AccountID = acct.ID
		if !c.Kind.Valid() {
			c.Kind = domain.ConversationFriend
		}
		if err := ingestTalker(ctx, adb, mode, cursors[talker], c, byTalker[talker]); err != nil {
			return err
		}
		if hooks.afterTalker != nil {
			if err := hooks.afterTalker(talker); err != nil {
				return err
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return domain.ErrBackupCancelled
	}
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return err
	}
	for _, m := range snap.Media {
		if err := ctx.Err(); err != nil {
			return domain.ErrBackupCancelled
		}
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

func ingestTalker(ctx context.Context, adb *sqlite.AccountDB, mode domain.BackupMode, cur domain.BackupCursor, c domain.Conversation, msgs []domain.Message) error {
	return adb.InTx(ctx, func(tx *sqlite.AccountDB) error {
		for _, m := range msgs {
			_, err := tx.GetMessage(ctx, m.TalkerID, m.MsgID)
			exists := err == nil
			if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
				return err
			}
			if exists {
				// Upsert body; never count this row as new.
				if err := tx.PutMessage(ctx, m); err != nil {
					return err
				}
				continue
			}
			if skipByCursor(mode, cur, m) {
				continue
			}
			if err := tx.PutMessage(ctx, m); err != nil {
				return err
			}
		}

		n, err := tx.CountMessages(ctx, c.TalkerID)
		if err != nil {
			return err
		}
		latest, err := tx.ListMessages(ctx, c.TalkerID, nil, 1)
		if err != nil {
			return err
		}
		var last time.Time
		var lastID string
		if len(latest) > 0 {
			last = latest[0].CreateTime
			lastID = latest[0].MsgID
		}

		existing, err := tx.GetConversation(ctx, c.TalkerID)
		if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			return err
		}
		if errors.Is(err, sqlite.ErrNotFound) {
			existing = c
		} else {
			existing.DisplayName = firstNonEmpty(c.DisplayName, existing.DisplayName)
			existing.Avatar = firstNonEmpty(c.Avatar, existing.Avatar)
			if c.Kind.Valid() {
				existing.Kind = c.Kind
			}
		}
		existing.AccountID = c.AccountID
		existing.MsgCount = n
		if last.After(existing.LastMsgTime) {
			existing.LastMsgTime = last
		}
		if err := tx.PutConversation(ctx, existing); err != nil {
			return err
		}

		cur.AccountID = c.AccountID
		cur.TalkerID = c.TalkerID
		if last.After(cur.LastEndTime) {
			cur.LastEndTime = last
		}
		if lastID != "" {
			cur.SegmentMeta = lastID
		}
		cur.Received = n
		if n > cur.Total {
			cur.Total = n
		}
		return tx.PutCursor(ctx, cur)
	})
}

func skipByCursor(mode domain.BackupMode, cur domain.BackupCursor, m domain.Message) bool {
	if mode == domain.BackupModeFull {
		return false
	}
	return messageAtOrBeforeCursor(cur, m)
}

func messageAtOrBeforeCursor(cur domain.BackupCursor, m domain.Message) bool {
	if cur.LastEndTime.IsZero() && cur.SegmentMeta == "" {
		return false
	}
	if !cur.LastEndTime.IsZero() {
		if m.CreateTime.Before(cur.LastEndTime) {
			return true
		}
		if m.CreateTime.Equal(cur.LastEndTime) {
			return cur.SegmentMeta == "" || m.MsgID == cur.SegmentMeta
		}
		return false
	}
	return m.MsgID == cur.SegmentMeta
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

func hasTalker(convs []domain.Conversation, talker string) bool {
	for _, c := range convs {
		if c.TalkerID == talker {
			return true
		}
	}
	return false
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func filterChunks(chunks []Chunk, req domain.BackupRequest) []Chunk {
	if req.Mode == domain.BackupModeFull || len(req.Cursors) == 0 {
		return chunks
	}
	byTalker := make(map[string]domain.BackupCursor, len(req.Cursors))
	for _, c := range req.Cursors {
		byTalker[c.TalkerID] = c
	}
	out := make([]Chunk, 0, len(chunks))
	for _, ch := range chunks {
		cur, ok := byTalker[ch.TalkerID]
		if !ok {
			out = append(out, ch)
			continue
		}
		kept := ch
		kept.Messages = nil
		for _, m := range ch.Messages {
			if messageAtOrBeforeCursor(cur, m) {
				continue
			}
			kept.Messages = append(kept.Messages, m)
		}
		if len(kept.Messages) == 0 {
			continue
		}
		out = append(out, kept)
	}
	return out
}
