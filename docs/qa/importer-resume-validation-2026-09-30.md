# Applied importer validation — 30 September

Root executed these checks against actual applied source using repository-local GOCACHE, pinned lint and real isolated PostgreSQL on loopback 55432. These are implementation checks, not independent Functional QA or global acceptance.

- Massage bridge: 1 PASS event. Massage PostgreSQL selection: 25 PASS events, no FAIL/SKIP. Shared adminmessage/importdelivery lint: exit0. Importer lint after formatting: exit0 (`r105-importer-lint3.log`). Original reviewer179 confirmed formatting equivalence without claiming execution.
- Full-history candidate: all before/final hashes guarded before application. One dependency changed only by formatting, independently checked by reviewer179. Focused CLI/PostgreSQL group: 88 PASS events, no FAIL/SKIP, exit0 (`r109-messages-pg1.jsonl`). Importer lint after pinned formatting: exit0 (`r109-importer-lint2.log`). Reviewer186 confirmed 11 applied files exact and four line-ending-only.
- Full importer module after these changes: exit0, 368 PASS / 1 SKIP events (`r109-importer-full1.jsonl`). `TestSnapshotSymlinkIsRejected` skipped because the Windows toolchain cannot create symlinks; a separate reparse validation test exists, but does not convert this skip into a pass.

Logs and counts are preserved in `qa.local/go-resume-20260929`. Test-event totals include package outcomes and subtests; they are not numbers of independent requirements. Runtime history/ACL/removal/restart, Telegram-like EN/RU flows and complete cross-domain data reconciliation remain open. No production operations were performed.

