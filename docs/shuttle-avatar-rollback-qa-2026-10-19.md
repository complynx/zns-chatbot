# Legacy avatar rollback hardening — 2026-10-19

Migration20 Down now obtains an ACCESS EXCLUSIVE NOWAIT table lock before its
emptiness check. Goose's migration transaction holds that lock through DROP.
Active table use rejects rollback; otherwise another writer cannot commit a
first avatar between the check and removal. Down continues to reject any populated avatar table. Export and retain bytes
and receipts before any separate, deliberate destructive removal.

The original code was exercised with an uncommitted synthetic avatar insert.
Rollback waited for that writer and hit the test deadline rather than returning
lock-not-available. This demonstrates the unsafe ordering; the test cancelled
before any intentional data removal. The failed pre-fix command is retained.

After the fix, both focused scenarios passed: empty Down/Up followed by refusal
for committed generation history, and immediate refusal during an uncommitted
insert followed by successful writer commit. Stored JPEG bytes and original
receipt replay remain unchanged. The command also reran all eighteen existing
avatar generation/transform/draft scenarios: twenty roots passed in total.
Project integration lint and an independent fresh code review passed.

These are integration checks with real isolated PostgreSQL and mock peers.
Production Go code and every other release input are byte-identical to the prior
accepted draft stage; only migration20 SQL changed. No manifest/schema revision
was added, no new image was built, and the primary schema49/mvp56 runtime was
not installed or modified. Source remains schema55/mvp62. This does not claim
safety of every historical migration or completed editor/runtime acceptance.

Ignored evidence: `.cache/shuttle-work/avatar-legacy-rollback-v1-qa/`, including
`pg-before`, `pg-after`, `lint.log`, review and source gate. Both own QA database
containers and volumes were archived and removed without OOM. Main runtime
identity/configuration/domain checks remained intact. The index remains empty.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
