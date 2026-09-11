# Findings

### F-001
- title: WxBackup is a fnOS-packaged Go service plus Vite/WeUI SPA
- severity: n/a_re
- category: design
- status: validated
- evidence_ids: [E-002, E-003, E-004]
- location: FPK manifest + server/WxBackup + dist/cgi/index.html
- impact: Reconstruction should target Go backend + Vue SPA + fnOS FPK/Docker packaging, not a CGI-only static app.
- confidence: high
- repro_steps:
  1. Unpack FPK and read manifest/cmd/main
  2. `file` the main binary and CGI helper
  3. Inspect dist/cgi and dist/port index.html
- remediation: n/a

### F-002
- title: Product core is “login as a computer + official LAN backup”, not phone filesystem dump
- severity: n/a_re
- category: reverse_algo
- status: validated
- evidence_ids: [E-001, E-006]
- location: README login/backup flow; WeChatProto.Backup* strings
- impact: Rebuild must isolate DeviceSession behind an adapter. Copying the Mac/iPad protocol from this binary is out of scope.
- confidence: high
- repro_steps:
  1. Read README “扫码登录后…通过微信官方自带的备份功能”
  2. Confirm BackupStartRequest / BakChatCreateQRCodePack strings
- remediation: n/a

### F-003
- title: Dual persistence: official WeChat backup files plus decoded viewer DB/index
- severity: n/a_re
- category: design
- status: validated
- evidence_ids: [E-001, E-005]
- location: Backup.db/BAK_* restore tutorial; wx_chat schema; search.index
- impact: Explains ~2x disk vs phone (issue #78). Independent rebuild can keep a single canonical store and export official format only when restoring.
- confidence: high
- repro_steps:
  1. README restore via Windows WeChat using Backup.db + BAK_*
  2. strings show wx_chat and bleve search.index
- remediation: n/a

### F-004
- title: Incremental backup is a per-talker cursor in wx_backup_history
- severity: n/a_re
- category: reverse_algo
- status: candidate
- evidence_ids: [E-005]
- location: wx_backup_history.segment_meta / endTime / get / total
- impact: Resume and incremental jobs should persist per-session offsets, not a single global byte pointer.
- confidence: medium
- repro_steps:
  1. strings for CREATE TABLE wx_backup_history and SELECT talkerId, segment_meta
- remediation: n/a
- notes: Single-source (binary strings). Residual: no live backup job observed.

### F-005
- title: HarmonyOS path is pinned to WeChat official ports 8011 and 24011
- severity: n/a_re
- category: design
- status: validated
- evidence_ids: [E-001, E-002]
- location: README FAQ; process must bind privileged ports as root
- impact: fnOS package needs host-network or explicit port publish; container NAT will break discovery.
- confidence: high
- repro_steps:
  1. README: 8011/24011 are official and cannot be changed
  2. privilege run-as root; service_port 20365
- remediation: n/a

### F-006
- title: Membership/pay is a separate commercial control plane
- severity: n/a_re
- category: other
- status: observed
- evidence_ids: [E-004]
- location: /api/products /api/pay /api/yearly; binary also references an external :20360 listener
- impact: Out of scope. Independent rebuild ships without vendor license checks.
- confidence: medium
- repro_steps:
  1. Frontend JS lists /api/products and /api/pay
- remediation: n/a
