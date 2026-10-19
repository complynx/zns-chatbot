# Agent event configuration plan

Bounded administration slice after
[worker duration acceptance](shuttle-worker-duration-qa-2026-10-15.md).
Architecture and Go consultations agreed on the design below. Migration 31,
the transactional store, agent routes and discovery are implemented and accepted
on the owned stand. All 242 integration race scenarios, other Go packages,
strict lint and fresh independent source reviews passed. Native real-IAM/Loom
exact creation/replacement, restart, original-receipt recovery and scoped
preservation checks passed after the retained failures below. The stand runs
schema 31 / manifest mvp-30. See [evidence and limits](shuttle-event-change-qa-2026-10-15.md).
This is bounded local acceptance, not production readiness.

Existing administrator agent `events` and `event` reads remain unchanged. Add
one dynamically discovered `change_event` write with prepare, confirm, status
and reconcile. Global administrator access is required on every call; event
authority or an assigned pass alone does not permit catalogue administration.

Arguments reuse `db.EventChange`: explicit event UUID, expected version and the
complete `events.Document`. Version zero creates; positive version replaces by
CAS. Normalize through `events.Normalize` before hashing and preparation. The
approval must show the exact normalized configuration, create versus replace,
event identity/version, UTC instants with the named display timezone, module
changes and optional policy removals. Omitted/null registration or deposit is
full-replacement removal, not “leave unchanged.”

Use a dedicated additive proposal table keyed by bot, organization, actor and
operation ID, with unique proposal UUID, canonical hash, original preparation,
five-minute database expiry and original optional result. The authenticated
scope is never accepted from tool arguments. Repeated preparation preserves
the original proposal and expiry; changed arguments conflict.

Use `beginAdminOperation` to serialize the operation and protect the current
exact active local actor. Current IAM role authorization remains the API's
responsibility. Confirmation must match operation/idempotency/proposal and
canonical arguments. A previously committed result returns its original event
version after current authorization, even if the event changed or the proposal
expired later. Missing status is unknown and requires same-operation recovery.

Reuse existing transaction-local `writeEvent` and `writeEventPolicies` directly
inside confirmation. Keep operator `ChangeEvent` and `shuttle_event_operations`
unchanged. The agent's original result belongs only to its proposal namespace;
an operator receipt with the same textual operation ID never proves agent
approval. Do not call the separately committing operator wrapper and then save
the agent result. Event row, child policies and agent result commit atomically.

Check database expiry after the event update or create-insert uniqueness wait,
while the transaction can still roll back. Existing versioned update and
`ON CONFLICT DO NOTHING` creation handle competing writers and occupied UUIDs.
Do not expose a foreign event merely because its UUID conflicts. Maintain
bounded operation timeouts and honest same-operation retry after deadlock or
storage uncertainty; do not claim deadlock immunity or reorder unrelated flows.

Preserve existing normalization: IANA zones, absolute microsecond instants,
sorted unique syntactically valid module IDs, exact integer pricing and valid
deposit minima across tiers. Do not add a module allowlist, new capacity floor
or past-event restrictions without a separate reason. Existing pass price,
currency, tier, deposit provenance and deadline snapshots, registration,
allocation, authority, payment ledger and historical receipts remain intact.
Current module access checks still apply after configuration changes.

Before choosing route/body/result limits, measure representative full module
and tier configurations against actual Loom broker limits. Existing global
8192-byte input and 16384-byte result defaults may be insufficient. Use bounded
method-specific limits and never truncate the exact approval or result. The
implemented limits are 128 KiB for prepare/confirm bodies and 256 KiB for tool
results. A full Go/Huma document with 100 maximum-length module IDs and 100
tiers measured 32,089 bytes for confirmation. The first preparation was
128,754 bytes because it repeated full documents in the description; native
Loom rejected that description before approval. The corrected preparation
measures 33,349 bytes and carries a bounded scalar summary plus complete
normalized arguments. A maximum-name test also covers escaped control
characters and multibyte names against Loom's 4,000-byte description limit.
Real Loom broker creation/replacement and recovery passed with complete
100-module/100-tier arguments; descriptions measured 1,144 and 1,149 bytes. Keep unrelated endpoint
limits unchanged. No new framework or dependency is needed.

Telegram approval delivery appends the complete pretty-printed arguments as
literal code before the final message's buttons. Existing rendering splits
messages within Telegram's UTF-16 limit; no parameters are truncated. A
presentation version changes both outbox deduplication and callback keys, so
a delivered legacy summary cannot suppress the new full presentation. Old
callbacks are rejected. Restarted controls known not to have been applied
are revalidated; controls with uncertain outcomes retain their original
idempotent request for reconciliation. Argument-free approvals retain their
existing callback keys.

Behavioral proof covers create/replace, policy clearing, effect-free prepare,
normalized replay, changed/foreign/missing/expired proposals, current role and
revocation denial, operator/agent namespace isolation, CAS/concurrent creates,
expiry after update/insert waits, rollback of configuration/policies/result,
lost-response reconciliation, restart and historical receipts after later edits.
Include an assigned pass whose frozen terms remain unchanged. Verify discovery,
strict nested schemas and a representative full document through the broker.

Finish with fresh source review loops, focused and required full checks, updated
documentation, and owned native exact approval of synthetic event creation and
replacement through Loom. Prove replay/restart, authorization and unchanged
pre-existing business data. Review fixture effects and exact cleanup before
execution. Migration down must refuse to discard proposal history; compensation
is a newly approved versioned replacement. Deletion APIs, allocation, additional
financial policy, production edits and cutover remain outside this slice.

Source review reproduced one wire-contract bug: Huma required the tier's
optional false flags. Explicit `required:"false"` tags now match the existing
decoder without changing JSON serialization. Raw HTTP tests cover omitted flags
and explicit null policy removal. The separate review claim that null policies
would fail was disproved by actual requests; optional null properties are
already accepted by Huma, so no nullability tags were changed. Real PostgreSQL
COMMIT-reply loss returns HTTP 503 and recovers the original event after restart
without another mutation. Assigned-pass terms and payment rows remain unchanged
through configuration replacement.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
