# Massage specialist notification store QA — 2026-10-18

The storage foundation passes ten focused integration scenarios on a disposable
PostgreSQL 17 instance with Go 1.27.2 and the race detector. This is component
acceptance. Delivery, agent/native preference endpoints and controls, and real
Telegram notification acceptance remain open. The main stand stays on schema 48
and mvp-55; migration 49 has not been applied there.

## Implemented behavior

Preferences belong to the bot account across its specialist events. An active
specialist binding in an explicitly selected, enabled event authorizes access;
a participant pass and global administrator status do not substitute for that
binding. Absence means notifications enabled at version zero. A value change
increments the version. A same-value write retains the version and stores an
immutable operation receipt. Agent and Mini App share CAS, authorization and
receipt recovery. Replaying an old receipt never reapplies an old setting.

Successful booking and cancellation create one transition-specific intent in
the business transaction. Missing bindings or a disabled preference create no
intent and receive no backfill. Each intent captures recipient, binding version
and preference version. Cancellation blocks a pending/retryable booking intent.
An insertion error rolls back the booking or cancellation and its receipt.
Migration rollback refuses to discard preferences, intents or operation history.

The future delivery reservation must recheck both captured generations and the
current enabled preference, account, module and booking state. Disable/re-enable
and revoke/regrant must suppress stale intents. A committed send reservation
cannot retract a message if cancellation or muting happens afterward. Cancelled
notices must not expose a Telegram contact link. These delivery conditions are
requirements, not claims established by this storage gate.

## Verification

Scoped Linux lint for `internal/db` and `integration` reports zero issues. Two
fresh correctness reviews and one separate quality review returned no actionable
findings. The final correctness review covered the formatted source. Reviewers
performed no tests or runtime operations. The test runner received a separate
textual review with no actionable findings. The index remained empty.

Ten integration roots passed, with sixteen passing test events and no failures
or skips. The gate covers shared native/agent CAS and historical receipts,
cross-event concurrency and regrant persistence, access denial and session
expiry, booking/cancellation replay through both interfaces, no backfill,
transaction rollback on notice failure, guarded migration rollback, and existing
owner reminder, binding and single-connection native staff behavior.

The disposable PostgreSQL has a 512 MiB limit; the Go runner has a 2 GiB limit.
Both use pinned images. Main Compose container identities, images, start times,
running states and restart counts were unchanged. This test gate does not claim
business-row fingerprint verification or main-application restart acceptance.
The temporary containers and their private database volume are removed after
successful acceptance; shared Go caches are retained.

Ignored evidence is under
`.cache/shuttle-work/massage-specialist-notice-store-v1-qa/`: checkpoint,
source snapshot, expected roots, race JSON log and lint output. The checkpoint
records exact resource ownership, terminal exit status and cleanup.

Next: implement generation-fenced delivery, outcome recovery and administrative
resolution, then the agent/native preference APIs and controls. Complete focused
multi-user mock Telegram acceptance before a reviewed main-stand installation.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
