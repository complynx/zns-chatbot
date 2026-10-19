# Runtime settings acceptance, 2026-10-15

This bounded slice adds agent `zinusya_administration.update_settings` with exact
approval and durable original receipts. It edits the existing application-wide
settings singleton. Startup YAML, environment variables and secrets are excluded.
This is local acceptance, not production readiness or deployment.

## Source and behavioral gates

Architecture and Go design consultations preceded implementation. The shared
settings validation/CAS/audit extraction preserved operator behavior. A narrow
shared administrative actor/operation lock supports settings and task closure;
no generic proposal engine was introduced.

Migration 29 adds dedicated actor/deployment/operation-scoped proposals and results.
Confirmation checks version, exact proposal/hash and expiry after settings-row
lock waits. Settings, existing audit and agent result commit atomically. Original
receipt replay precedes stale-version/expiry checks and does not restore old
settings. IAM TTL synchronization reads current database settings under the
process-local settings mutex and skips an already synchronized TTL.

Separate fresh store and full-slice correctness/quality reviews found no
actionable findings. Strict lint reported zero issues and `sqlc diff` passed.
All 206 enumerated integration tests passed in 14 bounded Linux/PostgreSQL race
batches; other explicit `cmd`, `internal` and `migrations` packages passed under
race as well. All 16 enumeration/package/batch containers exited zero without OOM.
The accepted source freeze contained 241 files and remained unchanged through
build and native verification.

New tests cover concurrent duplicate confirmation, operator/agent CAS contention,
invalid/changed/stale/expired proposals, actor/deployment isolation, revocation,
original receipts after newer settings and reopening, rollback on receipt-save
failure, default semantics, and migration down/up/audit retention. A lock-holder
test observes real PostgreSQL blocking before waiting for expiry and releasing
the settings row; confirmation then rejects without an audit or settings change.

A narrow PostgreSQL TCP proxy drops the actual successful `COMMIT` completion
before the client receives it. The API returns recoverable 503 while the database
contains one settings update, one audit and the original result. After restart,
status/reconcile and retry return that result without another mutation. This is
database acknowledgment loss, not a mocked internal return value.

Another fault test removes the current settings row after result persistence
inside the test transaction. The receipt commits, runtime synchronization fails,
the API returns 503 and positive authorization caching is disabled. After repairing
the synthetic fixture, reconciliation returns the original result. Cached status
polls during an IAM-peer outage and old-receipt replay after an operator TTL-zero
update are separately tested through HTTP behavior.

## Native owned stand

Candidate image:
`sha256:6e1916fe36be5d0067018e834f40b54b3b2ee66dbb717361022c3ccec6cf6007`.
Previous accepted image:
`sha256:7433abc66d9f6176a9aa723ef716c104d92f008fa21eb1217291a6211178ee5f`.
Pinned Go 1.27.2 compilation and packaging passed with a bounded builder and no
OOM. A custom-format application database dump was hashed and checked using
`pg_restore -l` before explicit migration from 28 to 29.

Native Loom run: `9a5c8614-04ec-4bb1-a14c-87e8860ea276`.
Operation: `239d565e-96a2-4f64-9565-ce4b7ca891bc`.

An existing isolated fixture administrator received a real delegated Zitadel
token. Loom discovered the new method, prepared the full settings replacement
and displayed exact approval. Before approval, version 1 and the original
document were unchanged. Approval committed version 2 with IAM TTL 120 seconds.
The provider received the validated result, and the same run completed.

After warming the authorization cache, the helper stopped only our own
`shuttle-live-test` Zitadel and observed its container actually stopped. Three
status polls still returned the original result; polling did not flush the cache.
Zitadel was restarted and readiness checked. The operator API then restored the
original document with TTL zero through CAS, creating version 3 and a second
audit entry. No version or audit history was reset.

Replaying the original confirmed operation still returned version 2/TTL 120 as
its historical receipt. A settings read followed by another confirmed owned IAM
outage returned 503, proving that the replay had not re-enabled a positive cache.
Zitadel was restarted again. Both outages targeted only the owned stand.

The previous binary started with schema 29 retained and read restored version 3.
After reinstalling the candidate, reconcile returned the original version-2
receipt. One proposal result and exactly two added settings audits remained.
All 22 selected pre-existing application table hashes and the Mini App origin
were unchanged; settings/audit/proposal tables intentionally record the two
audited changes. Existing Telegram test history was preserved.

Final independent read-back verified schema 29, manifest `mvp-28`, candidate image,
current version 3/TTL zero, original version-2/TTL-120 receipt, backup SHA, 241 source
hashes, nine owned services running without OOM and an empty due queue. Temporary
test PostgreSQL and its verified anonymous volume were removed after terminal
test gates. The main stand remains running.

## Evidence and limits

Ignored native evidence is under `.cache/shuttle-live-test`:
`native-settings-change-checkpoint.json`, `settings-change-before.dump` and
`settings-change-runtime-final.json`. The source freeze, authoritative 206-test
list, batch plan, exit/OOM states and lint/build logs are under
`.cache/shuttle-work`. These private fixture artifacts are not release artifacts.

Source review of the QA helper found a potential delayed-commit/cleanup race
before execution. Cleanup now fences this exact operation under the same advisory
lock and expires only an uncommitted proposal before restoring values. A fresh
review found no actionable findings. The successful native run needed no such
failure recovery; source review is the evidence for that helper failure path.

Native provider responses were manually authored, and this scenario sent no
Telegram messages. The checks do not prove unattended model judgment, propagation
to multiple API processes, instantaneous remote IAM revocation, browser trust or
bidirectional zitadeltg verification. Production rollout, the separate River
fast-start/stop lease observation and remaining roadmap policies stay open.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
