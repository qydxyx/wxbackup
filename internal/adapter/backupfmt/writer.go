package backupfmt

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
	_ "modernc.org/sqlite"
)

const (
	textMagic  = "WXT1"
	mediaMagic = "WXM1"
)

func Write(dir string, snap Snapshot) error {
	return WriteWithOptions(dir, snap, Options{})
}

func WriteWithOptions(dir string, snap Snapshot, opts Options) error {
	if dir == "" {
		return fmt.Errorf("backupfmt: dir is required")
	}
	max := opts.MaxShardSize
	if max <= 0 {
		max = defaultMaxShard
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := clearPackageFiles(dir); err != nil {
		return err
	}

	dbPath := filepath.Join(dir, BackupDBName)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err := db.Exec(`PRAGMA journal_mode=DELETE`); err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		return err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return err
	}

	if _, err := db.Exec(`INSERT INTO Meta(key, value) VALUES (?, ?), (?, ?), (?, ?), (?, ?)`,
		"format", InterchangeFormat,
		"version", InterchangeVersion,
		"account_id", snap.AccountID,
		"wxid", snap.WxID,
	); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, c := range snap.Conversations {
		if c.TalkerID == "" {
			return fmt.Errorf("backupfmt: conversation talker id is required")
		}
		if !c.Kind.Valid() {
			return fmt.Errorf("backupfmt: invalid conversation kind %q", c.Kind)
		}
		accountID := c.AccountID
		if accountID == "" {
			accountID = snap.AccountID
		}
		if _, err := tx.Exec(`
			INSERT INTO Session (talker_id, account_id, kind, display_name, avatar, last_msg_time, msg_count)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, c.TalkerID, accountID, string(c.Kind), c.DisplayName, c.Avatar, timeUnixMilli(c.LastMsgTime), c.MsgCount); err != nil {
			return err
		}
	}

	msgsByTalker := groupMessages(snap.Messages)
	text, err := newShardWriter(dir, "TEXT", textMagic, max)
	if err != nil {
		return err
	}
	defer text.Close()

	talkers := make([]string, 0, len(msgsByTalker))
	for t := range msgsByTalker {
		talkers = append(talkers, t)
	}
	sort.Strings(talkers)
	for _, talker := range talkers {
		msgs := msgsByTalker[talker]
		if err := writeTextSegments(tx, text, msgs); err != nil {
			return err
		}
	}
	if err := text.Close(); err != nil {
		return err
	}

	media, err := newShardWriter(dir, "MEDIA", mediaMagic, max)
	if err != nil {
		return err
	}
	defer media.Close()
	for _, m := range snap.Media {
		if m.MediaID == "" {
			return fmt.Errorf("backupfmt: media id is required")
		}
		if !m.Kind.Valid() {
			return fmt.Errorf("backupfmt: invalid media kind %q", m.Kind)
		}
		accountID := m.AccountID
		if accountID == "" {
			accountID = snap.AccountID
		}
		blob, err := mediaBytes(snap, m)
		if err != nil {
			return err
		}
		available := m.Available
		size := m.Size
		if len(blob) > 0 {
			available = true
			size = int64(len(blob))
			file, off, n, err := media.Append(blob)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`
				INSERT INTO MsgFileSegments (media_id, file, offset, length)
				VALUES (?, ?, ?, ?)
			`, m.MediaID, file, off, n); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`
			INSERT INTO MsgMedia (media_id, account_id, talker_id, kind, sha256, size, available)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, m.MediaID, accountID, "", string(m.Kind), m.SHA256, size, boolInt(available)); err != nil {
			return err
		}
	}
	if err := media.Close(); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

const schemaSQL = `
CREATE TABLE Meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE Session (
	talker_id TEXT PRIMARY KEY,
	account_id TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,
	display_name TEXT NOT NULL DEFAULT '',
	avatar TEXT NOT NULL DEFAULT '',
	last_msg_time INTEGER NOT NULL DEFAULT 0,
	msg_count INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE MsgSegments (
	id INTEGER PRIMARY KEY,
	talker_id TEXT NOT NULL,
	file INTEGER NOT NULL,
	offset INTEGER NOT NULL,
	length INTEGER NOT NULL,
	msg_count INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE MsgMedia (
	media_id TEXT PRIMARY KEY,
	account_id TEXT NOT NULL DEFAULT '',
	talker_id TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,
	sha256 TEXT NOT NULL DEFAULT '',
	size INTEGER NOT NULL DEFAULT 0,
	available INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE MsgFileSegments (
	id INTEGER PRIMARY KEY,
	media_id TEXT NOT NULL,
	file INTEGER NOT NULL,
	offset INTEGER NOT NULL,
	length INTEGER NOT NULL
);
`

type wireMessage struct {
	AccountID  string         `json:"account_id"`
	TalkerID   string         `json:"talker_id"`
	MsgID      string         `json:"msg_id"`
	MsgSeq     int64          `json:"msg_seq"`
	MsgType    int            `json:"msg_type"`
	IsSend     bool           `json:"is_send"`
	CreateTime int64          `json:"create_time"`
	Text       string         `json:"text"`
	XML        string         `json:"xml"`
	Extra      map[string]any `json:"extra,omitempty"`
}

func writeTextSegments(tx *sql.Tx, text *shardWriter, msgs []domain.Message) error {
	var (
		curFile  = -1
		curOff   int64
		curLen   int64
		curCount int
		talker   string
	)
	flush := func() error {
		if curCount == 0 {
			return nil
		}
		_, err := tx.Exec(`
			INSERT INTO MsgSegments (talker_id, file, offset, length, msg_count)
			VALUES (?, ?, ?, ?, ?)
		`, talker, curFile, curOff, curLen, curCount)
		curCount, curLen = 0, 0
		return err
	}
	for _, m := range msgs {
		if m.TalkerID == "" || m.MsgID == "" {
			return fmt.Errorf("backupfmt: talker id and msg id are required")
		}
		payload, err := json.Marshal(wireMessage{
			AccountID:  m.AccountID,
			TalkerID:   m.TalkerID,
			MsgID:      m.MsgID,
			MsgSeq:     m.MsgSeq,
			MsgType:    m.MsgType,
			IsSend:     m.IsSend,
			CreateTime: timeUnixMilli(m.CreateTime),
			Text:       m.Text,
			XML:        m.XML,
			Extra:      m.Extra,
		})
		if err != nil {
			return err
		}
		rec := make([]byte, 4+len(payload))
		binary.LittleEndian.PutUint32(rec[:4], uint32(len(payload)))
		copy(rec[4:], payload)

		file, off, n, err := text.Append(rec)
		if err != nil {
			return err
		}
		if curCount > 0 && (file != curFile || off != curOff+curLen) {
			if err := flush(); err != nil {
				return err
			}
		}
		if curCount == 0 {
			talker = m.TalkerID
			curFile = file
			curOff = off
		}
		curLen += n
		curCount++
	}
	return flush()
}

func groupMessages(msgs []domain.Message) map[string][]domain.Message {
	out := make(map[string][]domain.Message)
	for _, m := range msgs {
		out[m.TalkerID] = append(out[m.TalkerID], m)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool {
			a, b := out[k][i], out[k][j]
			if !a.CreateTime.Equal(b.CreateTime) {
				return a.CreateTime.Before(b.CreateTime)
			}
			if a.MsgSeq != b.MsgSeq {
				return a.MsgSeq < b.MsgSeq
			}
			return a.MsgID < b.MsgID
		})
	}
	return out
}

func mediaBytes(snap Snapshot, m domain.MediaObject) ([]byte, error) {
	if blob, ok := snap.MediaBlobs[m.MediaID]; ok {
		return blob, nil
	}
	if m.Path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(m.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

type shardWriter struct {
	dir    string
	kind   string
	magic  string
	max    int64
	index  int
	size   int64
	f      *os.File
	closed bool
}

func newShardWriter(dir, kind, magic string, max int64) (*shardWriter, error) {
	w := &shardWriter{dir: dir, kind: kind, magic: magic, max: max}
	if err := w.rotate(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *shardWriter) name(n int) string {
	return filepath.Join(w.dir, fmt.Sprintf("BAK_%d_%s", n, w.kind))
}

func (w *shardWriter) rotate() error {
	if w.f != nil {
		if err := w.f.Close(); err != nil {
			return err
		}
		w.f = nil
		w.index++
	}
	f, err := os.OpenFile(w.name(w.index), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(f, w.magic); err != nil {
		_ = f.Close()
		return err
	}
	w.f = f
	w.size = int64(len(w.magic))
	return nil
}

func (w *shardWriter) Append(p []byte) (file int, offset, n int64, err error) {
	if w.closed {
		return 0, 0, 0, fmt.Errorf("backupfmt: shard writer closed")
	}
	needRotate := w.size > int64(len(w.magic)) && w.size+int64(len(p)) > w.max
	if needRotate {
		if err := w.rotate(); err != nil {
			return 0, 0, 0, err
		}
	}
	off := w.size
	nw, err := w.f.Write(p)
	if err != nil {
		return 0, 0, 0, err
	}
	w.size += int64(nw)
	return w.index, off, int64(nw), nil
}

func (w *shardWriter) Close() error {
	if w == nil || w.closed {
		return nil
	}
	w.closed = true
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

func clearPackageFiles(dir string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		name := e.Name()
		if name == BackupDBName || isShardName(name) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func isShardName(name string) bool {
	// BAK_<n>_TEXT or BAK_<n>_MEDIA
	if len(name) < 8 || name[:4] != "BAK_" {
		return false
	}
	return len(name) >= 10 && (name[len(name)-5:] == "_TEXT" || name[len(name)-6:] == "_MEDIA")
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
