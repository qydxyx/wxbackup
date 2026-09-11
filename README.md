# wxbackup

NAS-side WeChat backup viewer and restore helper. Chat data stays on the machine you run this on.

```text
WXBACKUP_PORT=20365
WXBACKUP_DATA=./data
./server
```

Without a DeviceSession adapter, `POST /v1/accounts/{id}/backup` returns **503** `no_session`.

## Adapter: Sidecar (official WeChat Backup folder)

This tree does not speak the WeChat computer protocol and does not ship a UI-automation binary. The first DeviceSession implementation is a **sidecar directory watcher**.

It watches a user-configured folder for a complete official Windows/Mac WeChat backup package:

```text
Backup.db
BAK_0_TEXT  BAK_0_MEDIA
BAK_1_TEXT  BAK_1_MEDIA
...
```

`StartBackup` waits until `Backup.db` **and** at least one `BAK_*` shard are present and sizes have settled, then calls `backupfmt.Read`. Only a decoded **wxbackup-interchange** package becomes a `BackupStream` (one chunk per talker). Official SQLCipher `Backup.db` returns `backupfmt.ErrNeedsKey` and the backup job **fails** — it does not ingest an empty snapshot. This adapter does not derive session keys.

### Point WeChat's Backup folder at this directory

**Windows**

1. In WeChat: 备份与恢复 → 备份到电脑, and finish a backup at least once so the client creates the folder.
2. Typical locations (version-dependent):
   - `Documents\WeChat Files\<wxid>\Backup`
   - a path you chose in WeChat's backup settings
3. Either:
   - set WeChat's backup output directory to the sidecar path (symlink or change the folder WeChat writes), or
   - copy `Backup.db` and every `BAK_*` file into the sidecar directory after the phone backup finishes.

**macOS**

Same layout. Copy or bind-mount WeChat's Backup folder onto the sidecar path. Do not rename `Backup.db` / `BAK_*`.

Then start the server with that path:

```text
WXBACKUP_SIDECAR_DIR=/path/to/wechat-backup
WXBACKUP_DATA=./data
./server
```

Leave `WXBACKUP_SIDECAR_DIR` unset to keep backup APIs at 503 `no_session`. Copy the interchange files this tree writes (`format=wxbackup-interchange`), or a plaintext package `backupfmt.Read` can decode. A later capture/PCAP adapter (optional) is out of this package.
