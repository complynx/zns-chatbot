# Pass payment processing

Domain/API implemented. Telegram integration and independent acceptance pending.
No production writes or migration cutover have run.

An owner uploads immutable evidence through `POST /v1/pass-proofs?filename=...`.
This reuses the existing owner-bound file store (historical table name
`core.order_proofs`). There is no direct file-ID download route. Files remain
limited to 20 MiB; names reject control characters and path separators.

The upload body is the raw file, not JSON or multipart. The response contains the
immutable proof `id`. Submit it using `POST /v1/passes/actions`:

```json
{"name":"proof","event":"event-id","version":3,"key":"unique-operation-key","proof_id":"uploaded-proof-id"}
```

The command fields are `name`, `event`, `version`, `key`, and the optional
`target`, `target_version`, `invite_telegram_id`, `payment_admin`, `proof_id`,
`payment_attempt`. Obtain versions and attempt IDs from current authorized reads.
The bearer identifies the actor; the JSON body does not select one.

Administrative reassignment uses `POST /v1/passes/admin/assign` with required
`event`, `key`, `version`, `target`, `target_version`. Optional fields are
`total_price`, `kind`, `comment`, `skip_balance`, `append_tier`, and
`create: {from_profile, role?, legal_name?, profile_version}`. `create` explicitly
requests a missing application for a known user. `total_price` is the total for
all assigned members, including a reciprocal pair. This route requires global
booking-admin rights; it does not grant payment review rights.

The `proof` registration command contains the owner's current booking version,
an idempotency key and `proof_id`. Only an assigned participant can submit.
A reciprocal assigned couple changes atomically to `paid`. As in Python, this
means evidence was received, not that an administrator approved it. The attempt
stores submitter, immutable file, receiving administrator, timestamp and the
original participant/assignment snapshot. A replacement creates a new attempt;
the previous attempt and decision remain available for audit.

`proof_accept` and `proof_reject` require event payment-admin membership, including
hidden administrators. Global booking-admin rights alone grant no review right.
Both take `target`, `target_version` and `payment_attempt`; `version` remains the
acting user's own booking version (zero when absent). Approval records reviewer
and timestamp. Rejection returns current participants to `assigned`. The event
lock serializes reviews, replacement, cancellation and allocation. A stale
attempt cannot review a replacement. Idempotent replays return current state.

Current, receiving and reviewing administrators are separate concepts. Changing
the current contact must not rewrite receiving/review provenance. Cancelling a
participant detaches their current proof, without deleting historical evidence.
Review of a surviving participant never recreates or changes a detached booking.

Read endpoints:

- `GET /v1/passes/events/{event}/participants/{owner}/payment`: current attempt
  and target version, restricted to that owner or an event payment administrator.
- The same path with `/file`: authorized current attachment as a download with
  no-store/nosniff headers and version/attempt response headers.
- `GET /v1/passes/events/{event}/payment-queue?after=...`: payment administrators
  only; 25 pending attempts per page, one item per couple receipt, current
  participant as review target, opaque deletion-safe continuation.

An imported paid, zero-price assignment with the `free_pass` marker can have no
payment attempt or source timestamps. Its current payment view uses `kind: free`
and `decision: accepted`; `received_at` is null when absent, and absent review
metadata remains omitted. Existing source timestamps retain their original
instants. Native attempt timestamps remain JSON timestamps. This read does not
create an attempt, uploader, receipt, reviewer or timestamp. The metadata must
match the current assignment; the receipt download remains forbidden.

PostgreSQL tests cover pair atomicity, immutable replacement, owner/file
isolation, hidden-admin review, global-admin denial, concurrent accept/reject,
stale attempts, replay, cancelled-partner preservation, and download permission
revocation. Test success is not independent Functional QA acceptance.

Remaining: Telegram proof selection/review/file delivery, notification outbox,
deadline jobs, free/forced assignments, full admin operations and import/export.
