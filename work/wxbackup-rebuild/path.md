### P-001
- title: Observed backup-to-viewer callflow
- path_type: callflow
- start: User opens fnOS iframe CGI
- goal: Phone chat history stored on NAS and browsable offline
- steps:
  1. action: fnOS desktop opens /cgi/ThirdParty/WxBackup/index.cgi (Vite SPA) — evidence: E-002, E-004 — finding: F-001
  2. action: First-time login on Web; QR + optional slider via headless Chromium — evidence: E-001, E-006 — finding: F-002
  3. action: Backend maintains a WeChat computer session (Mac/iPad protocol client) — evidence: E-006 — finding: F-002
  4. action: User starts backup; phone WeChat stays foreground on same LAN; NAS listens on 8011/24011 and 20360-20367 — evidence: E-001, E-005 — finding: F-005
  5. action: Stage “正在备份” streams official backup packets; written as Backup.db + BAK_* — evidence: E-001, E-005 — finding: F-003
  6. action: Stage “整理数据/更新数据” decrypts/parses into wx_chat/wx_friend and bleve search.index while session still logged in — evidence: E-001, E-005 — finding: F-003, F-004
  7. action: Viewer reads /api/msg/* from local SQLite; restore either replays LAN protocol or hands Backup.db to Windows WeChat — evidence: E-001, E-004 — finding: F-002
- residual_risks: Live protocol capture on own devices not performed this turn; DeviceSession internals remain behind the adapter boundary.
