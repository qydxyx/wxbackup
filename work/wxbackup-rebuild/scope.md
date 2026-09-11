# Case Scope

## meta
- case_id: wxbackup-rebuild
- created: 2026-09-12T01:38:31+08:00
- operator: local
- project_root: /Users/qydxyx/projects/wxbackup
- primary_skill: reverse-engineering/SKILL.md
- primary_id: R0
- lead_role: lead
- specialist_roles: [cre, doc]
- hint: Public-doc architecture reverse of NAS WeChat backup (weibeifen/wxbackup README + public WeChat backup protocol + similar OSS). Produce independent reconstruction code plan. Out of scope: membership crack, proprietary binary clone, attacking WeChat production.
- preset: none

## auth
- status: granted
- basis: own_system
- evidence_of_auth: User-owned workspace wxbackup; analysis of public GitHub README/releases and public protocol/OSS; independent reconstruction plan (no license bypass)
- MUST NOT proceed if status != granted

## in_scope
- assets:
  - https://github.com/weibeifen/wxbackup
  - public WeChat mobile-to-PC backup protocol (LAN)
  - public OSS WeChat backup/viewer projects
- surfaces: [docs, nas_package, web, binary_strings]
- activities: [recon, reverse, report]

## out_of_scope
- assets: [WeChat production login/backup servers, vendor license control plane]
- activities: [dos, phishing_real_users, unrestricted_exfil, membership_bypass, proprietary_protocol_clone, decompile_to_copy]

## network_profile
- mode: authorized_target_only
- notes: |
    offline | lab_only | authorized_target_only | unrestricted_lab
    Change mode only after auth.status = granted.
    Presets: offline-sample | ctf-public | own-system

## deliverables
- report: true
- field_journal: true
- diagrams: true
- timeline: true

## constraints
- timebox: {}
- stealth: low
- data_handling: anonymize

## signoff
- ready_for_act: true
- checklist:
  - [x] auth.status = granted
  - [x] in_scope.assets non-empty OR offline sample path set
  - [x] network_profile.mode chosen
  - [x] out_of_scope reviewed
  - [x] roles assigned (see skills/ops/role-map.md)

## ops_refs
- skills/ops/scope-contract.md
- skills/ops/evidence-finding-path.md
- skills/ops/role-map.md
- skills/ops/timeline-workitem.md
- skills/ops/IDENTITY.md
