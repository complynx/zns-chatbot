# Historical registration discovery QA — 2026-10-15

Scope: discover bounded, structurally supported candidates in one owned archived
users record without knowing its legacy keys. This adds owner-only
`zinusya_history.registrations` alongside the existing singular read. It does not
verify that candidates were registrations, establish current eligibility, confirm
money or complete migration. See the [contract](shuttle-history-registration.md).

Architect and senior Go consultation preceded implementation. The selected
approach reuses the existing strict projector and complete archive validator.
Unsupported shapes and their keys are omitted; a role-only object can qualify.
These limits are explicit in the manifest and documentation. No database schema,
stored history projection, derived index or dependency was added.

The shared source and database helpers preserve singular lookup, owner locking,
archive membership/hash checks, cancellation and final IAM/binding checks. Two
independent source reviews reported no actionable findings. Strict lint reported
zero issues after bounded formatting, constant reuse and extraction of the page
projector. The first lint failure is retained in local evidence. The index stayed
empty; no commit, push or release was made.

Focused Linux Go 1.27.2 race checks passed against real disposable PostgreSQL.
They cover literal dotted and Unicode keys, exclusive cursors, empty/final pages,
false versus absent payment assertions, unsupported/private data omission,
revocation, rebinding, foreign administrators, final authorization failure,
cancellation, one connection, and late archive corruption. A source record with
more than 4096 total top-level fields fails closed. A maximum page with worst-case
JSON escaping fits the actual advertised 256 KiB result budget.

All 259 enumerated integration scenarios now have verified successful race
receipts across 18 bounded groups; the other explicit Go packages also passed.
The first attempt stopped in batch 11 when the existing payment-notification
fairness scenario exceeded its eight-second delivery wait. That runner exited 1
without OOM. Three isolated race repetitions passed, followed by a successful
repeat of the complete group with the original parallelism. The cause of the
initial timeout is unproven; no production code or test timeout was changed to
obtain those passes. Both initial and subsequent receipts are retained.
The final coverage check matched every test name exactly once in the successful
group receipts. This is a complete verified test inventory with a retained failed
attempt, not a claim that the initial full attempt passed without retries.
Completed runners were removed after their terminal records were saved. The
disposable PostgreSQL exited 0 without OOM and was removed with its anonymous
volume. The owned application stand remains available.

Owned native acceptance passed using real Zitadel and Loom. A private PostgreSQL
backup preceded replacement of only the bot container. Schema remained 32;
manifest became `mvp-32`. Build provenance covers 213 production paths, including
embedded inputs, with identical hashes before and after the bounded build.
The candidate reuses the accepted certificates, avatar executable and licenses;
this is not a full production Dockerfile or deployment gate. The accepted image is
`sha256:afb2450bf30ca7baf98c13b785f835b3ba55b9df64a8c4532c3dead7e79f0b3b`.

Native exact-approved storage/apply added one synthetic users history record.
All preexisting legacy row hashes remained present and unchanged. Ordinary-owner
HTTP reads returned three one-item pages, preserving false and true paid claims
and the role-only candidate while excluding an unsupported object and a private
bank canary. A foreign administrator received 404. The existing singular method
returned the same fields for a discovered key.

A fresh Loom chat then used actual `script` dispatch to find the source record
through `history.page`, discover its first key without receiving any key in advance,
follow two cursors and call the singular method with the discovered key. The
results matched the direct API reads. Every answered provider request was checked
to be a pending model step for that run. The run completed under the manual test
provider. A separate read-only review of this harness found no actionable findings
before execution. This proves real tool dispatch, not unattended model reasoning.

Archive/history hashes stayed unchanged during reads. Live business and bot
inbox/task/outbox/conversation fingerprints stayed unchanged throughout. Loom
container identity/start time and bot/Loom configuration hashes were preserved.
No Telegram message, authorization change, production mutation or developer Loom
stand change was needed. This slice adds no restart/fault-injection or shutdown
acceptance. CA/zitadeltg interaction and selection of a real export remain open.

Ignored evidence is under `.cache/shuttle-work/history-discovery-qa`, the matching
`container-results` batch plan/logs, and
`.cache/shuttle-live-test/history-discovery-qa/checkpoint.json`. These are local
acceptance artifacts, not CI or release artifacts.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
