package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
)

func (a *AccountDB) PutConversation(ctx context.Context, c domain.Conversation) error {
	if c.TalkerID == "" {
		return fmt.Errorf("sqlite: talker id is required")
	}
	if !c.Kind.Valid() {
		return fmt.Errorf("sqlite: invalid conversation kind %q", c.Kind)
	}
	accountID, err := a.bindAccountID(c.AccountID)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO conversations (talker_id, account_id, kind, display_name, avatar, last_msg_time, msg_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(talker_id) DO UPDATE SET
			kind=excluded.kind,
			display_name=excluded.display_name,
			avatar=excluded.avatar,
			last_msg_time=excluded.last_msg_time,
			msg_count=excluded.msg_count
	`, c.TalkerID, accountID, string(c.Kind), c.DisplayName, c.Avatar, timeUnixMilli(c.LastMsgTime), c.MsgCount)
	return mapSQLError(err)
}

func (a *AccountDB) GetConversation(ctx context.Context, talkerID string) (domain.Conversation, error) {
	return scanConversation(a.db.QueryRowContext(ctx, conversationSelect+` WHERE talker_id = ?`, talkerID))
}

func (a *AccountDB) ListConversations(ctx context.Context) ([]domain.Conversation, error) {
	rows, err := a.db.QueryContext(ctx, conversationSelect+` ORDER BY last_msg_time DESC, talker_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *AccountDB) PutMessage(ctx context.Context, m domain.Message) error {
	if m.TalkerID == "" || m.MsgID == "" {
		return fmt.Errorf("sqlite: talker id and msg id are required")
	}
	accountID, err := a.bindAccountID(m.AccountID)
	if err != nil {
		return err
	}
	extra, err := marshalExtra(m.Extra)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO messages (talker_id, msg_id, account_id, msg_seq, msg_type, is_send, create_time, text, xml, extra)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(talker_id, msg_id) DO UPDATE SET
			msg_seq=excluded.msg_seq,
			msg_type=excluded.msg_type,
			is_send=excluded.is_send,
			create_time=excluded.create_time,
			text=excluded.text,
			xml=excluded.xml,
			extra=excluded.extra
	`, m.TalkerID, m.MsgID, accountID, m.MsgSeq, m.MsgType, boolInt(m.IsSend), timeUnixMilli(m.CreateTime), m.Text, m.XML, extra)
	return mapSQLError(err)
}

func (a *AccountDB) GetMessage(ctx context.Context, talkerID, msgID string) (domain.Message, error) {
	return scanMessage(a.db.QueryRowContext(ctx, messageSelect+` WHERE talker_id = ? AND msg_id = ?`, talkerID, msgID))
}

// MessageCursor is the keyset bound for ListMessages (create_time, msg_seq, msg_id DESC).
// A nil cursor means the newest page; Unix epoch is a real timestamp, not "no cursor".
type MessageCursor struct {
	CreateTime time.Time
	MsgSeq     int64
	MsgID      string
}

func CursorFromMessage(m domain.Message) MessageCursor {
	return MessageCursor{CreateTime: m.CreateTime, MsgSeq: m.MsgSeq, MsgID: m.MsgID}
}

func (a *AccountDB) ListMessages(ctx context.Context, talkerID string, before *MessageCursor, limit int) ([]domain.Message, error) {
	if talkerID == "" {
		return nil, fmt.Errorf("sqlite: talker id is required")
	}
	if limit <= 0 {
		limit = defaultMsgPage
	}
	if limit > maxMsgPage {
		limit = maxMsgPage
	}
	q := messageSelect + `
		WHERE talker_id = ?
		ORDER BY create_time DESC, msg_seq DESC, msg_id DESC
		LIMIT ?`
	args := []any{talkerID, limit}
	if before != nil {
		ct := timeUnixMilli(before.CreateTime)
		q = messageSelect + `
			WHERE talker_id = ?
			  AND (
			    create_time < ?
			    OR (create_time = ? AND msg_seq < ?)
			    OR (create_time = ? AND msg_seq = ? AND msg_id < ?)
			  )
			ORDER BY create_time DESC, msg_seq DESC, msg_id DESC
			LIMIT ?`
		args = []any{talkerID, ct, ct, before.MsgSeq, ct, before.MsgSeq, before.MsgID, limit}
	}
	rows, err := a.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (a *AccountDB) PutMedia(ctx context.Context, m domain.MediaObject) error {
	if m.MediaID == "" {
		return fmt.Errorf("sqlite: media id is required")
	}
	if !m.Kind.Valid() {
		return fmt.Errorf("sqlite: invalid media kind %q", m.Kind)
	}
	accountID, err := a.bindAccountID(m.AccountID)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO media (media_id, account_id, kind, sha256, path, size, available)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(media_id) DO UPDATE SET
			kind=excluded.kind,
			sha256=excluded.sha256,
			path=excluded.path,
			size=excluded.size,
			available=excluded.available
	`, m.MediaID, accountID, string(m.Kind), m.SHA256, m.Path, m.Size, boolInt(m.Available))
	return mapSQLError(err)
}

func (a *AccountDB) GetMedia(ctx context.Context, mediaID string) (domain.MediaObject, error) {
	return scanMedia(a.db.QueryRowContext(ctx, mediaSelect+` WHERE media_id = ?`, mediaID))
}

func (a *AccountDB) ListMedia(ctx context.Context) ([]domain.MediaObject, error) {
	rows, err := a.db.QueryContext(ctx, mediaSelect+` ORDER BY media_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MediaObject
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (a *AccountDB) PutCursor(ctx context.Context, c domain.BackupCursor) error {
	if c.TalkerID == "" {
		return fmt.Errorf("sqlite: talker id is required")
	}
	accountID, err := a.bindAccountID(c.AccountID)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO backup_cursors (talker_id, account_id, last_end_time, segment_meta, received, total)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(talker_id) DO UPDATE SET
			last_end_time=excluded.last_end_time,
			segment_meta=excluded.segment_meta,
			received=excluded.received,
			total=excluded.total
	`, c.TalkerID, accountID, timeUnixMilli(c.LastEndTime), c.SegmentMeta, c.Received, c.Total)
	return mapSQLError(err)
}

func (a *AccountDB) GetCursor(ctx context.Context, talkerID string) (domain.BackupCursor, error) {
	return scanCursor(a.db.QueryRowContext(ctx, cursorSelect+` WHERE talker_id = ?`, talkerID))
}

func (a *AccountDB) ListCursors(ctx context.Context) ([]domain.BackupCursor, error) {
	rows, err := a.db.QueryContext(ctx, cursorSelect+` ORDER BY talker_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BackupCursor
	for rows.Next() {
		c, err := scanCursor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

const conversationSelect = `SELECT talker_id, account_id, kind, display_name, avatar, last_msg_time, msg_count FROM conversations`

const messageSelect = `SELECT talker_id, msg_id, account_id, msg_seq, msg_type, is_send, create_time, text, xml, extra FROM messages`

const mediaSelect = `SELECT media_id, account_id, kind, sha256, path, size, available FROM media`

const cursorSelect = `SELECT talker_id, account_id, last_end_time, segment_meta, received, total FROM backup_cursors`

func scanConversation(sc rowScanner) (domain.Conversation, error) {
	var c domain.Conversation
	var kind string
	var last int64
	err := sc.Scan(&c.TalkerID, &c.AccountID, &kind, &c.DisplayName, &c.Avatar, &last, &c.MsgCount)
	if err != nil {
		return domain.Conversation{}, mapNotFound(err)
	}
	c.Kind = domain.ConversationKind(kind)
	c.LastMsgTime = unixMilliTime(last)
	return c, nil
}

func scanMessage(sc rowScanner) (domain.Message, error) {
	var m domain.Message
	var isSend int
	var created int64
	var extra string
	err := sc.Scan(&m.TalkerID, &m.MsgID, &m.AccountID, &m.MsgSeq, &m.MsgType, &isSend, &created, &m.Text, &m.XML, &extra)
	if err != nil {
		return domain.Message{}, mapNotFound(err)
	}
	m.IsSend = isSend != 0
	m.CreateTime = time.UnixMilli(created).UTC()
	m.Extra, err = unmarshalExtra(extra)
	if err != nil {
		return domain.Message{}, err
	}
	return m, nil
}

func scanMedia(sc rowScanner) (domain.MediaObject, error) {
	var m domain.MediaObject
	var kind string
	var avail int
	err := sc.Scan(&m.MediaID, &m.AccountID, &kind, &m.SHA256, &m.Path, &m.Size, &avail)
	if err != nil {
		return domain.MediaObject{}, mapNotFound(err)
	}
	m.Kind = domain.MediaKind(kind)
	m.Available = avail != 0
	return m, nil
}

func scanCursor(sc rowScanner) (domain.BackupCursor, error) {
	var c domain.BackupCursor
	var end int64
	err := sc.Scan(&c.TalkerID, &c.AccountID, &end, &c.SegmentMeta, &c.Received, &c.Total)
	if err != nil {
		return domain.BackupCursor{}, mapNotFound(err)
	}
	c.LastEndTime = unixMilliTime(end)
	return c, nil
}

func marshalExtra(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalExtra(s string) (map[string]any, error) {
	if s == "" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func timeUnixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func unixMilliTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
