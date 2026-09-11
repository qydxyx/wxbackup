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

Leave `WXBACKUP_SIDECAR_DIR` unset to keep backup APIs at 503 `no_session`. Copy the interchange files this tree writes (`format=wxbackup-interchange`), or a plaintext package `backupfmt.Read` can decode.

## Adapter: Capture (optional, own-device PCAP)

`internal/adapter/devicesession/capture` is a **stub**. It is not wired into the server. It must not be implemented from a third-party FPK or a decompiled WeChat Mac/iPad protocol stack.

LAN capture is a **new authorized scope**: only the operator's own phone and the operator's own NAS on the same Wi-Fi, offline/lab. Production WeChat servers and other people's devices are out of scope. Until that grant exists, `CaptureSession` returns `ErrNotAuthorized`. After the grant, it still returns `ErrNotImplemented` until a parser is written against own-device fixtures — `StartBackup` never returns a stream and never binds `8011`/`24011`.

### How to add own-device PCAPs later

1. Confirm the extra scope in writing (own phone + own NAS only). Do not scan or probe WeChat production hosts.
2. On the NAS (or the machine that will be the "computer"), capture while the phone runs 备份到电脑 on the same LAN:

   ```text
   tcpdump -i <lan> -s 0 -w own-phone-nas.pcapng \
     port 8011 or port 24011
   ```

   Expected phases in the trace: **discovery** (8011/24011), **transfer**, **heartbeat**. Keep the phone WeChat in the foreground.
3. Drop the file in `internal/adapter/devicesession/capture/testdata/pcaps/`. That directory gitignores `*.pcap` / `*.pcapng` so chat traffic is not committed. Use a local, redacted copy for tests.
4. Implement `capture.Parser` from those fixtures only (phase list, then frame layout). Do not paste protobuf from the paid WxBackup ELF. `CaptureSession` should keep refusing live backup until that parser has tests against the fixtures.
