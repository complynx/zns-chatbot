# Avatar document delivery — source implementation

Source schema57 / manifest mvp65 includes a task-free avatar document worker.
It consumes committed finalization intents through the existing River service;
no Loom run, event or task outbox record is required. The installed primary is
still schema49/mvp56. This source has not been installed there or in production.

Shared [native/agent finalization APIs](shuttle-avatar-finalization-api.md),
owner status/content reads and bounded native-action context are implemented.
The [Mini App Generate/send UI](shuttle-avatar-send-ui.md) is implemented with
isolated browser proof. [Owned runtime/browser QA](shuttle-avatar-runtime-qa-2026-10-19.md)
now passes all four paths with real IAM/Loom and mock Telegram. Actual Telegram
WebView and unattended model acceptance remain open.

## Send and authorization

Ready pending/retryable intents are scanned in bounded pages with an independent
cursor. Queue insertion uses delivery ID/version as unique arguments. Committed
intents are rediscovered after missed enqueue or restart. Jobs contain no image
bytes, token or user profile.

The worker verifies the current owner through server-side IAM, then reserves
under the owner lock. Reservation rechecks local revocation, deployment/subject
and equality between the current Telegram user and the captured recipient.
Revocation or recipient change blocks the intent, including changes during IAM
I/O. Temporary IAM failure snoozes the job; it does not regenerate the artifact
or create a Loom run. Native session expiry after admission does not revoke an
otherwise eligible owner's durable send.

The reservation binds an immutable artifact/receipt to its owner and delivery,
checks stored content hash, size and JPEG signature, then records send_inflight,
a new version and an attempt fence before Telegram. The actual network request
uses those persisted bytes and captured chat ID, with filename zinusya-avatar.jpg.
No mutable Telegram source or draft is fetched during delivery.

## Attempts, duplicate jobs and recovery

Finish requires the exact delivery ID, reserved version, attempt fence and
send_inflight state. A stale worker cannot finish a later attempt. Definite
not-started/retryable results retain a due retry timestamp; explicit Telegram
retry-after is respected. Ambiguous HTTP/receipt outcomes become delivery_unknown.
A wrong chat in a nominal success receipt is also unknown. Unknown, failed and
blocked intents are not automatically requeued as fresh sends.

The attempt fence is a non-secret deterministic UUID derived from delivery ID,
River job ID and the version being attempted. It identifies the reserving job;
it grants no authorization. Fixed in-process lock stripes bound memory and keep
live/rescued executions from reconciling a still-active send. Contention snoozes
a job rather than completing its pending work. Another job cannot finish an
inflight attempt as if it owned it.

If sending returns but recording its outcome fails, retry of the original job
can recognize its own abandoned attempt once the local gate is free and persist
unknown, without another Telegram request. It conservatively loses certainty
rather than guessing whether the earlier outcome was safe to resend. This
in-process recovery follows River's finite attempt budget. If persistence keeps
failing until the original job is discarded, application restart is required for
startup recovery; prolonged PostgreSQL failures are not claimed to be handled
indefinitely in process.

Startup, under the existing exclusive application-instance lock and before queue
workers start, converts remaining inflight intents to unknown. There is no timed
send lease that authorizes a second sender. Result persistence uses a detached
context bounded to one second during shutdown. Service stop keeps its existing
instance-lock lifecycle and waits for cancelled workers before release.

The [eventless administrative resolution store](shuttle-avatar-delivery-resolution.md)
now supports GlobalAdmin prepare/confirm, version CAS, reason/audit history and
possible-duplicate warnings. Its [agent HTTP tools](shuttle-avatar-delivery-resolution.md#agent-http-contract) now provide inspection, prepare/confirm and receipt reconciliation. Existing generic
notification repair remains event-bound; no fake event was introduced.

## Observability and proof

The worker registers bounded operation and worker-duration labels as
avatar_deliveries / zinusya_avatar_delivery. Queue due-count/age collection
includes the new job kind. These are metrics; this stage does not claim a new
cross-service trace contract.

Behavioral acceptance uses real isolated PostgreSQL/River with mock IAM/Loom/TG.
It exercises queued native admission with Loom down, expired native sessions,
source loss, delivery and restart, unknown/wrong-chat receipts, duplicate jobs
during a live send, retry-after, revoked/changed/IAM-denied recipients, eligibility
changes during IAM I/O, transient IAM failure, startup recovery, stale attempts,
graceful stop during a send and failure to record a Telegram outcome.

The initial38-root command passed. Review then found that a failed finish write
could strand inflight work until restart; a focused trigger-injection test
reproduced it before the job-bound recovery/gate fix. The next broad command
passed40 of41 roots: its duplicate-job fixture incorrectly required scheduled
state, while River deliberately keeps short snoozes available. The final fixture
requires an attempted, postponed job in either supported state and still checks
that the live delivery is unchanged and sends once. These attempts remain
separate. Final PostgreSQL/Linux/lint counts and cleanup are in the source gate.

Ignored evidence is `.cache/shuttle-work/avatar-delivery-v1-qa/`, including
separate PostgreSQL dumps/logs, initial/final lint, reviews and focused failure
reproduction. No real Telegram, installed real IAM/Loom runtime acceptance,
production changes, publication or image installation occurred in this stage.
[Owned runtime completion](shuttle-avatar-runtime-qa-2026-10-19.md) now passes native-only,
agent-only and both handoffs. Actual Telegram WebView and unattended model remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
