# Order notice delivery store

This page records the persistence-stage checkpoint. The subsequent
[worker, rendering and administrative recovery](shuttle-order-notice-delivery.md)
now have separate verification and are installed on the owned schema45 stand.
[Native-only Telegram acceptance](shuttle-order-proof-native-qa-2026-10-17.md)
passed; agent/mixed and fault-path live acceptance remain open. The later
[subprocess recovery gate](shuttle-order-notice-process-qa-2026-10-17.md) uses
real PostgreSQL and mock peers. No agent run is initiated by delivery.

## Retained events and delivery state

Migration 44 extends the notice rows created atomically by evidence admission and
human review in schema 41. It preserves each notice ID, evidence/recipient/kind
uniqueness, original document and creation time. Existing inserts use the new
pending/version-1 defaults. The evidence snapshot remains the original admission
or decision; the order is decoded from its immutable submitted result, including
sold event name and timezone. Current event edits do not rewrite those terms.

Pending/retryable notices are discovered in bounded UUID pages. Reservation uses
the expected version and a fresh attempt ID. It commits send_inflight before any
Telegram IO. Completion accepts only that attempt and increments the version.
A stale completion cannot finish a later attempt. Recovery, called only while the
application owns its deployment instance lock, changes interrupted attempts to
delivery_unknown. It never automatically requeues an uncertain send.

Delivery does not confirm money, release holds, occupy resources, cancel orders
or perform refunds. This persistence checkpoint did not add administrative retry/dismiss. The
subsequent runtime adds that explicit path; unknown notices stay quarantined
until an authorized administrator resolves them.

## Current access

Reads and repair are deployment scoped. Reservation reads the canonical target,
then locks the event and current recipient account. It repeats module, subject,
revocation and event authority checks after any preceding caller waits. The
runtime worker verifies IAM before this reservation.

An owner may receive historical notices without a currently assigned pass.
Other recipients must still be event ambassadors/administrators. Revoking the
payer does not suppress historical notices to a different, authorized reviewer;
a revoked recipient cannot receive them. If review supersedes an admission,
the old admission notice is ineligible rather than reported as current status.

The down migration refuses to remove attempted delivery history. Untouched
pending/version-1 rows can return to their original shape without deleting their
business documents. Binary rollback and mixed-version runtime acceptance have
not been performed.

## Verification and remaining acceptance

The final Linux Go 1.27.2/PostgreSQL 17 run passed 74 root tests and 151 pass
events with the race detector, no skips/failures/OOM, and 931 unchanged frozen
inputs. It includes three new behavioral roots plus existing order evidence,
DB and task checks. These cover access loss after initial lookup, deployment and
subject isolation, pass removal, immutable sold terms, stale completion,
retryable attempts, recovery quarantine and refusal to discard delivery history.
Recovery is exercised through the store; this is not full application restart QA.

Scoped DB/integration lint passed. sqlc compile passed and generation was
reproducible. Independent correctness, quality and final reviews reported no
actionable findings. The v1 SQL ambiguity, v2 initializer compilation error and
v3 lint-only failure are retained separately in ignored local QA artifacts.

The owned main stand remains at schema 40, verified after the gate. Its ten
container fingerprints did not change. Temporary test containers and their
volume were removed. At this store checkpoint, source schema was 44 and the API manifest remained
mvp-43 because no API contract changed. The subsequent runtime is schema 45 /
manifest mvp-44. Real native/agent/mixed flow and notice fault
acceptance require a later owned candidate activation. Production readiness is
not established; publication remains on hold.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
