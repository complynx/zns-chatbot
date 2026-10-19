# Durable Telegram prelink storage

## Implemented boundary

Migration 33 and `internal/db/telegram_prelink.go` implement the local durable
part of the [trusted prelink design](shuttle-telegram-prelink-plan.md). The
[IAM link adapter](shuttle-telegram-link-qa-2026-10-16.md) is a separate network
phase. The [private backend endpoint](shuttle-telegram-prelink-api.md) is now implemented;
the [provider hook/private TLS route](shuttle-zitadeltg-prelink-provider.md) and
[combined real-IAM fixtures](shuttle-prelink-combined-qa-2026-10-16.md) have scoped acceptance. Database methods must only receive
freshly verified, server-scoped provider assertions.

`ClaimTelegramPrelink` commits the canonical local user and pending receipt before
IAM work. `CompleteTelegramPrelink` follows fresh IAM account/link verification,
locks USER then RECEIPT, checks current identity/revocation/proof expiry, and binds
the IAM subject plus completed receipt in one transaction. No method holds a DB
connection while the caller performs IAM requests. A completed receipt does not
replace fresh proof verification, local authority checks or IAM link read-back.

The canonical-user upsert and row lock are shared with Mini App under generic
Telegram names. Their SQL behavior is unchanged: one UUID per bot/numeric user,
and an existing profile is preserved. Provider intake creates no conversation,
queue job or agent request. Existing Telegram intake still owns its usual inbox
and conversation behavior.

## Receipt and replay

`shuttle_telegram_prelinks` keeps a UUID, bot/user, provider issuer/JTI, exact
compact-proof SHA-256, configured organization/IdP, exact external subject,
expiry and pending/completed result. It stores neither a raw assertion nor a
service bearer. `(provider_issuer, assertion_jti)` is unique. Same proof and scope
recover the same receipt; changed hash, user, provider, organization, external
subject or expiry cannot repurpose it. A fresh JTI may reconcile the same account.

Proof timestamps are absolute instants with whole-second precision, matching
JWT NumericDate. The DB uses its own clock and limits remaining freshness to 65s
(the verifier's 60s lifetime plus 5s skew). It checks expiry again after
waiting for locks and before completion. Expiry never silently deletes intent or
resurrects an old assertion. Revoked users are denied for both pending and
completed receipts. A conflict leaves the other user's binding and pending intent
unchanged through transaction rollback.

## Migration and compatibility

The [owned main stand now runs schema33](shuttle-prelink-main-qa-2026-10-16.md),
with existing rows preserved and the receipt table empty. Its prelink endpoint
remains disabled. Production has not been migrated.
The new partial unique index covers `(bot_id, zitadel_org, zitadel_subject)` for
nonempty subjects. It protects old SQL writers as well as the new completion
path. Existing duplicate groups make migration fail; no account is selected,
merged, unlinked or deleted automatically. A read-only main-stand audit found no
duplicate groups before this stage.

The migration adds one table and an index; existing row data is preserved. New
receipt calls need schema 33. Do not run them against 32 or roll schema back while
new callers are active. Binary startup/rollback compatibility must be checked
when a deployable endpoint image is prepared; old SQL acceptance alone is not
proof that every older executable can restart against33.

Down migration takes an exclusive receipt-table lock and refuses any populated
receipt table, including pending intent. A normal rollback cannot discard accepted
identity operations. Export and separately authorized clearance are required
before that destructive rollback can proceed. An empty receipt table permits
down/up; the rollback removes only the new table/index. Keep the current runtime
image and database backups for a reviewed deployment, never reinitialize IAM.

## Verification

The focused receipt and existing Mini App behavioral tests passed on isolated
real PostgreSQL under Linux race detection, with no skips. Coverage includes all three
entry orders at the local-user layer, fresh-connection recovery with a one-slot
pool, concurrent reserve/complete, immutable replay scope, revocation, true expiry,
atomic duplicate-subject denial and older-writer protection. Migration checks
proved empty down/up, refusal to discard pending intent, and failure on duplicate
old bindings with both records preserved.

Strict DB-package lint reports 0 issues. Pinned sqlc compile/generate succeeds and
regeneration preserves both generated files byte-for-byte. Independent source
review found no actionable issue.

The first broad run exhausted its PostgreSQL fixture's 512 MiB limit during
integration setup. Its logs and confirmed OOM state are retained. That fixture was
removed and recreated with 1536 MiB, 64 MiB shared buffers and bounded WAL. On the
clean retry, command/internal/migration packages passed; the integration binary
later ended with `signal: killed` without an individual test failure. PostgreSQL
stayed healthy. Because the test container was auto-removed, its final cgroup
state was unavailable; the SIGKILL cause is not confirmed.

A separate complete integration run passed in a retained 4 GiB container with a
768 MiB Go heap target and recorded memory/cgroup evidence. All 261 root tests
matched the independently listed package inventory and passed, with no skips.
Its terminal exit was 0; OOM state and cgroup `oom`, `oom_kill`, `max` counters were 0.
Surrounding command/internal/migration packages passed on the clean retry. The
only surrounding-suite skip was the `TestWatchdogHelper` entrypoint, intended for
its parent's child-process invocation. This is broad schema 33 regression proof;
it does not accept the combined endpoint/provider login flow.

Ignored evidence lives under `.cache/shuttle-work/prelink-db-qa/`: targeted logs,
both unsuccessful broad attempts, confirmed first PostgreSQL OOM state, the second
unconfirmed SIGKILL, full integration inventory/results/resource samples,
`acceptance.json` and final cleanup states. Earlier failures remain failures; no
product source change was made to disguise either execution-resource observation.

The exact temporary Go/PostgreSQL containers and their owned network were removed
after evidence capture. No persistent test volume or host port was created. The
main stand kept schema 32, unchanged business hashes/task counts and unchanged
app/IAM/Loom/PostgreSQL/gateway container IDs/start times. Its database was unaffected
by the fixture OOM or subsequent test-process SIGKILL. No runtime image, main-stand
migration, Telegram message, commit or push was part of this storage stage.

Provider [assertion verification](shuttle-telegram-prelink-verifier.md) is now
implemented separately. The [authenticated endpoint/startup wiring](shuttle-telegram-prelink-api.md) are
now implemented separately. The isolated hook/private TLS connection, real-IAM
convergence and scoped recovery have their [combined](shuttle-prelink-combined-qa-2026-10-16.md)
and [resilience](shuttle-prelink-resilience-qa-2026-10-16.md) evidence. Main provider/backend activation
and interactive Telegram/browser acceptance remain open; local transaction tests
do not prove the full login flow.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
