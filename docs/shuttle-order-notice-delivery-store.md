# Order notice delivery store

This page records the persistence-stage checkpoint.
No agent run is initiated by delivery.

## Retained events and delivery state

Migration 44 extends the notice rows created atomically by evidence admission and
human review in schema 41. It preserves each notice ID, evidence/recipient/kind
uniqueness, original document and creation time. Existing inserts use the new
pending/version-1 defaults. The evidence snapshot remains the original admission
or decision; the order is decoded from its immutable submitted result, including
sold event name and timezone. Current event edits do not rewrite those terms.

Pending/retryable notices are discovered in bounded UUID pages.
It commits send_inflight before any
Telegram IO.
Completion accepts only that attempt and increments the version.
Recovery, called only while the
application owns its deployment instance lock, changes interrupted attempts to
delivery_unknown.
It never automatically requeues an uncertain send.

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

The down migration refuses to remove attempted delivery history.
Untouched
pending/version-1 rows can return to their original shape without deleting their
business documents.
