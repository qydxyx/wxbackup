package backup

import "github.com/wxbackup/wxbackup/internal/adapter/devicesession"

// Chunk is one talker segment from a DeviceSession BackupStream.
type Chunk = devicesession.Chunk

// ChunkSource is an optional BackupStream extension used by the sidecar and fake adapters.
type ChunkSource = devicesession.ChunkSource

// FakeSession is the test DeviceSession. Implementation lives in adapter/devicesession.
type FakeSession = devicesession.FakeSession
