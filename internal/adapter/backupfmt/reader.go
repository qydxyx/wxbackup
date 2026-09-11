package backupfmt

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
)

var sqliteMagic = []byte("SQLite format 3\x00")

// Read loads a backup package. If Backup.db or BAK shards cannot be decoded
// without a session key, Conversations may still be filled and the error is
// ErrNeedsKey; Messages and MediaBlobs are left empty.
func Read(dir string) (Snapshot, error) {
	var zero Snapshot
	dbPath := filepath.Join(dir, BackupDBName)
	st, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, ErrNotPackage
		}
		return zero, err
	}
	if st.IsDir() {
		return zero, ErrNotPackage
	}

	ok, err := looksLikeSQLite(dbPath)
	if err != nil {
		return zero, err
	}
	if !ok {
		return zero, ErrNeedsKey
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return zero, ErrNeedsKey
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if err := db.Ping(); err != nil {
		return zero, ErrNeedsKey
	}

	hasSession, err := tableExists(db, "Session")
	if err != nil {
		return zero, err
	}
	if !hasSession {
		return zero, ErrNotPackage
	}

	snap, err := readSessions(db)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrNeedsKey, err)
	}

	format, _ := metaValue(db, "format")
	if format != InterchangeFormat {
		// Official / unknown index: metadata only. Do not treat BAK bytes as plaintext.
		return snap, ErrNeedsKey
	}
	snap.AccountID, _ = metaValue(db, "account_id")
	snap.WxID, _ = metaValue(db, "wxid")
	for i := range snap.Conversations {
		if snap.Conversations[i].AccountID == "" {
			snap.Conversations[i].AccountID = snap.AccountID
		}
	}

	msgs, err := readMessages(db, dir)
	if err != nil {
		return snap, err
	}
	snap.Messages = msgs

	media, blobs, err := readMedia(db, dir)
	if err != nil {
		return snap, err
	}
	snap.Media = media
	snap.MediaBlobs = blobs
	return snap, nil
}

func looksLikeSQLite(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, len(sqliteMagic))
	n, err := io.ReadFull(f, buf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return n == len(sqliteMagic) && bytes.Equal(buf, sqliteMagic), nil
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	return n > 0, err
}

func metaValue(db *sql.DB, key string) (string, error) {
	var v string
	err := db.QueryRow(`SELECT value FROM Meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func readSessions(db *sql.DB) (Snapshot, error) {
	rows, err := db.Query(`
		SELECT talker_id, account_id, kind, display_name, avatar, last_msg_time, msg_count
		FROM Session ORDER BY talker_id`)
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	var snap Snapshot
	for rows.Next() {
		var c domain.Conversation
		var kind string
		var last int64
		if err := rows.Scan(&c.TalkerID, &c.AccountID, &kind, &c.DisplayName, &c.Avatar, &last, &c.MsgCount); err != nil {
			return Snapshot{}, err
		}
		c.Kind = domain.ConversationKind(kind)
		c.LastMsgTime = unixMilliTime(last)
		snap.Conversations = append(snap.Conversations, c)
	}
	return snap, rows.Err()
}

func readMessages(db *sql.DB, dir string) ([]domain.Message, error) {
	has, err := tableExists(db, "MsgSegments")
	if err != nil || !has {
		return nil, err
	}
	rows, err := db.Query(`SELECT talker_id, file, offset, length, msg_count FROM MsgSegments ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		var talker string
		var file int
		var off, length int64
		var count int
		if err := rows.Scan(&talker, &file, &off, &length, &count); err != nil {
			return nil, err
		}
		payload, err := readShardRange(dir, "TEXT", file, off, length, textMagic)
		if err != nil {
			return out, err
		}
		msgs, err := decodeTextRecords(payload)
		if err != nil {
			return out, err
		}
		out = append(out, msgs...)
	}
	return out, rows.Err()
}

func readMedia(db *sql.DB, dir string) ([]domain.MediaObject, map[string][]byte, error) {
	has, err := tableExists(db, "MsgMedia")
	if err != nil || !has {
		return nil, nil, err
	}
	rows, err := db.Query(`SELECT media_id, account_id, kind, sha256, size, available FROM MsgMedia ORDER BY media_id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var media []domain.MediaObject
	for rows.Next() {
		var m domain.MediaObject
		var kind string
		var avail int
		if err := rows.Scan(&m.MediaID, &m.AccountID, &kind, &m.SHA256, &m.Size, &avail); err != nil {
			return nil, nil, err
		}
		m.Kind = domain.MediaKind(kind)
		m.Available = avail != 0
		media = append(media, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	hasSeg, err := tableExists(db, "MsgFileSegments")
	if err != nil || !hasSeg {
		return media, nil, err
	}
	segRows, err := db.Query(`SELECT media_id, file, offset, length FROM MsgFileSegments ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	defer segRows.Close()
	blobs := make(map[string][]byte)
	for segRows.Next() {
		var mediaID string
		var file int
		var off, length int64
		if err := segRows.Scan(&mediaID, &file, &off, &length); err != nil {
			return media, blobs, err
		}
		b, err := readShardRange(dir, "MEDIA", file, off, length, mediaMagic)
		if err != nil {
			return media, blobs, err
		}
		blobs[mediaID] = append(blobs[mediaID], b...)
		for i := range media {
			if media[i].MediaID == mediaID {
				media[i].Path = fmt.Sprintf("BAK_%d_MEDIA", file)
				media[i].Size = int64(len(blobs[mediaID]))
				media[i].Available = true
			}
		}
	}
	return media, blobs, segRows.Err()
}

func readShardRange(dir, kind string, file int, off, length int64, magic string) ([]byte, error) {
	path := filepath.Join(dir, fmt.Sprintf("BAK_%d_%s", file, kind))
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: missing %s", ErrNeedsKey, filepath.Base(path))
		}
		return nil, err
	}
	defer f.Close()
	head := make([]byte, len(magic))
	if _, err := io.ReadFull(f, head); err != nil || string(head) != magic {
		return nil, fmt.Errorf("%w: %s is not plaintext interchange", ErrNeedsKey, filepath.Base(path))
	}
	if length < 0 {
		return nil, fmt.Errorf("backupfmt: negative segment length")
	}
	buf := make([]byte, length)
	if length == 0 {
		return buf, nil
	}
	n, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n != int(length) {
		return nil, fmt.Errorf("%w: short read from %s", ErrNeedsKey, filepath.Base(path))
	}
	return buf, nil
}

func decodeTextRecords(buf []byte) ([]domain.Message, error) {
	var out []domain.Message
	for len(buf) > 0 {
		if len(buf) < 4 {
			return nil, fmt.Errorf("backupfmt: truncated text record")
		}
		n := binary.LittleEndian.Uint32(buf[:4])
		buf = buf[4:]
		if uint32(len(buf)) < n {
			return nil, fmt.Errorf("backupfmt: truncated text payload")
		}
		payload := buf[:n]
		buf = buf[n:]
		var w wireMessage
		if err := json.Unmarshal(payload, &w); err != nil {
			return nil, fmt.Errorf("backupfmt: message json: %w", err)
		}
		out = append(out, domain.Message{
			AccountID:  w.AccountID,
			TalkerID:   w.TalkerID,
			MsgID:      w.MsgID,
			MsgSeq:     w.MsgSeq,
			MsgType:    w.MsgType,
			IsSend:     w.IsSend,
			CreateTime: unixMilliTime(w.CreateTime),
			Text:       w.Text,
			XML:        w.XML,
			Extra:      w.Extra,
		})
	}
	return out, nil
}

func unixMilliTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
