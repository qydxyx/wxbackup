// Package backupfmt reads and writes the Windows WeChat restore directory layout:
//
//	Backup.db
//	BAK_0_TEXT  BAK_0_MEDIA
//	BAK_1_TEXT  BAK_1_MEDIA
//	...
//
// Official WeChat restore packages are typically SQLCipher-encrypted; the key
// comes from a live computer-WeChat session, not from this package. Writer
// therefore emits an unencrypted sqlite interchange (wxbackup-interchange) for
// our export/import and tests. Reader never derives or guesses keys: without a
// readable sqlite header or without our shard framing it returns session
// metadata only plus ErrNeedsKey, and does not invent message bodies.
package backupfmt

import (
	"errors"

	"github.com/wxbackup/wxbackup/internal/domain"
)

const (
	BackupDBName = "Backup.db"

	// InterchangeFormat is stored in Meta so Reader can tell our plaintext
	// sqlite from an official SQLCipher dump that happens to share table names.
	InterchangeFormat  = "wxbackup-interchange"
	InterchangeVersion = "1"

	defaultMaxShard = 4 << 20
	// Hard cap on a single MsgSegments/MsgFileSegments length so a hostile
	// index cannot OOM the importer with make([]byte, length).
	maxSegmentBytes = 64 << 20
)

var (
	// ErrNeedsKey is returned when Backup.db or BAK shards are encrypted or
	// otherwise unreadable without a session key. Snapshot conversations may
	// still be populated from a readable Session table.
	ErrNeedsKey = errors.New("backupfmt: encrypted or unreadable without a session key")

	// ErrNotPackage means the directory has no Backup.db (or it is not a backup index).
	ErrNotPackage = errors.New("backupfmt: directory is not a Backup.db package")
)

// Snapshot is one backup package in domain types.
type Snapshot struct {
	AccountID     string
	WxID          string
	Conversations []domain.Conversation
	Messages      []domain.Message
	Media         []domain.MediaObject
	// MediaBlobs is optional payload keyed by media_id. Writer copies these
	// bytes into BAK_*_MEDIA; Reader fills them when shards are plaintext.
	// Reader leaves MediaObject.Path empty — the shard file is not a locator
	// for one object — so a Read→Write cycle must use MediaBlobs (or a real
	// filesystem Path supplied by the caller).
	MediaBlobs map[string][]byte
}

// Options control Writer layout. Zero value uses defaults.
type Options struct {
	// MaxShardSize is the target size of each BAK_N_* file before rotation.
	// A single record may exceed it. Zero means 4MiB.
	MaxShardSize int64
}
