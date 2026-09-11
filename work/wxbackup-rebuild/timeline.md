# Timeline (append-only)

## 2026-09-12T01:38:31+08:00 | lead | init
- action: case-init
- command_or_ref: skills/scripts/case-init.sh
- result_summary: case directory created; scope ready_for_act=true
- artifacts: [scope.md, workitems.md]
- evidence_ids: []
- decision_delta: [case_initialized]
- carry_forward_refs: [scope.md]
- next: open PRIMARY SKILL.md and ACT within scope

## 2026-09-12T01:44:00+08:00 | cre | triage+static
- action: README + FPK metadata + Go/Vite strings; no dynamic run
- command_or_ref: gh release download; tar; file; strings; objdump
- result_summary: Go ELF NAS app; computer-protocol backup receiver; dual store; Vue API surface recovered
- artifacts: [samples/WxBackup_1.2.9.0_x86.fpk, evidence/E-001.md, evidence/E-006.md]
- evidence_ids: [E-001, E-002, E-003, E-004, E-005, E-006]
- decision_delta: [phase=triage->static->synthesis; skip-dynamic=no-device-session; rebuild=clean-room-adapter]
- carry_forward_refs: [scope.md, evidence/E-001.md, evidence/E-006.md]
- next: write reconstruction plan and architecture report

## 2026-09-12T01:50:00+08:00 | doc | synthesis
- action: emit architecture report + independent rebuild plan
- command_or_ref: docs/2026-09-12_reverse-wxbackup-architecture-report.md
- result_summary: F-001..F-006, P-001, 16-PR rebuild plan
- artifacts: [../../docs/2026-09-12_wxbackup-rebuild-plan.md, ../../docs/2026-09-12_reverse-wxbackup-architecture-report.md]
- evidence_ids: [E-001, E-002, E-003, E-004, E-005, E-006]
- decision_delta: [deliverable=rebuild-plan]
- carry_forward_refs: [scope.md, findings.md, path.md]
- next: user reviews plan; optional own-device PCAP for DeviceSession adapter
