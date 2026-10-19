# Specialist notification process recovery QA — 2026-10-18

Accepted on a separate disposable `zinusya-mock-notice-restart` stand with the
current schema49 / `mvp-56` bot image. This gate proves safe retry after process
restart and recovery of an actual inflight send after SIGKILL. Telegram is a local
simulator; PostgreSQL, IAM, native application endpoints and the bot process are
real. No real Telegram message was sent by this gate.

## Controlled Telegram acknowledgment

The simulator now supports `accepted_then_hold`: it commits a message and attempt,
then withholds the response while control and independent sends remain usable.
Authenticated `/qa/release` releases the exact attempt. Repeated release is
idempotent across separate SQLite connections. Holds expire after 1 to 60 seconds
(default 60); expiry disconnects while retaining the accepted effect. Sixteen
unreleased holds cap this fault's outstanding resources. Durable hold metadata is
not evidence that its HTTP connection remains active.

Two focused HTTP cases first failed with unsupported-fault400, then passed after
implementation. The complete simulator/operator suite passes20 tests. Independent
correctness review found a cross-connection release race; a conditional UPDATE and
concurrent HTTP check fixed it. A fresh review and separate safety review were
clean. The [operator guide](../deploy/shuttle/mock_tg/README.md) covers arming,
transcripts and exact release; production configuration needs no additional field.

## Fixture and recovery evidence

The new stand used pinned local bot/Loom/file-worker images, its own database,
IAM organization, CA, fake bot token and mailbox. Its nine services had explicit
resource limits and no OOM event. Synthetic users501 and502 opened native sessions
through signed mock initData and real IAM. SQL explicitly seeded a synthetic event,
schedule, assigned participant pass and private specialist binding. Notification
states, booking effects and send outcomes were never seeded by SQL. Bookings and
cancellations used real native prepare and exact confirmation. No Loom inference
was requested.

The first new booking produced an actual specialist send rejected by mock429 with
retry_after30. Before stopping, its durable intent was `retryable`: one rejected
attempt, no accepted message. The same bot container stopped gracefully in0.734
seconds, exit0. The intent remained retryable while the process was stopped. That
same container was started; all eight dependencies kept their IDs and start epochs.
After the recorded deadline the intent became delivered. There were two attempts
(the429 and successful retry), exactly one accepted effect and a matching receipt.
The fixture booking was then cancelled through the native API.

For the second booking, `accepted_then_hold` committed one mock message while the
bot's actual intent remained `send_inflight`. The readiness check jointly observed
the database state, matching accepted message and unreleased mock attempt. A prior
review had caught a QA race that checked database reservation before mock acceptance;
that check was corrected and freshly reviewed before execution.

SIGKILL terminated that exact bot container, exit137. Its notification stayed
`send_inflight` while stopped. Starting the same container changed it to
`delivery_unknown`, preserving its attempt UUID and incrementing its version once.
The mock still had one attempt and one accepted effect: no automatic resend.
Releasing the old response did not replace the unknown outcome. The second booking
was cancelled through the native API; its ordinary cancellation notice was accepted
locally, and the original booked intent remained unknown afterward.

Both fixture bookings ended cancelled. Four messages and five sendMessage attempts
were retained: the first booking's rejected/successful pair, its cancellation,
the second booking's accepted-but-unacknowledged send and its cancellation. Both
Mini App sessions were logged out and the temporary session file was deleted.
A real operator CLI release of the already-released attempt returned success with
identical mock state and no new journal event.

## Cleanup, isolation and limits

The accepted state was exported as a302052-byte custom database archive, with SHA256
retained and rechecked, alongside the final mock transcript and original frozen
manifest. This is retained evidence, not a new restore acceptance claim. The nine
temporary services were stopped without OOM, their terminal IDs retained, then only
this project's containers, volumes and network were removed after ownership checks.
The generic stand tool retains ignored fixture credentials/configuration; the
per-user native session credentials were removed. No running temporary worker remains.

Fresh checks against the primary `shuttle-live-test` baseline matched112 raw
fingerprints except the allowed River leader/queue bookkeeping. Its bot and poller
fingerprints and Loom run count were unchanged, and task/outbox quiet checks passed.
This gate changed no production or Loom developer stand. The Git index remains
empty; no commit, push or publication occurred.

Ignored evidence:
`.cache/shuttle-work/mock-tg-hold-v1-qa/` contains initial failure metadata,20-test
logs, source review gate and before/after primary isolation receipts.
`.cache/shuttle-work/specialist-notice-mock-restart-v1-qa/` contains the accepted
checkpoint, recorded native operations, real worker/mock snapshots, reviewed driver
gate, CLI release proof, archive hash, original manifest, terminal container receipts
and resource cleanup proof. The original manifest is retained because intentional
stand retirement updates its lifecycle manifest after acceptance.

This accepts a retryable specialist notice after graceful stop and an actual inflight
specialist notice after crash. Never-attempted pending notices, other task/outbox
families, simultaneous Loom/IAM outages, mute/rebind generation fences, full live
rollback and browser Mini App controls remain separate gates. The
[real Telegram happy path](shuttle-massage-specialist-notices-real-qa-2026-10-18.md)
provides transport/rendering evidence; this isolated process gate does not replace it.
No unattended-model or production-readiness claim is made.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
