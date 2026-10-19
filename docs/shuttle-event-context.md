# Participant event context

Introduced in `mvp-6`, current manifest revision `mvp-8` exposes `zinusya_context.resolve`, `.list`, `.choose`
and the operation status endpoint. These methods use the authenticated account;
arguments cannot select another user. Context suggests an event. Every business
operation must still use an explicit event ID and recheck access.

Selection order is explicit event, eligible saved run choice, eligible saved user
choice, contextual hint, ongoing event, then nearest future event. An unavailable
explicit event is rejected; unavailable saved choices and hints are skipped.
Several ongoing events or equal nearest start times require clarification.
`require_explicit` permits explicit and saved choices but disables hints and
automatic selection. Resolve does not persist its suggestion.

Events must belong to this bot and organization, have the requested module enabled,
and not have ended. Registration context requires configured, open registration
sales. Other participant modules currently require an assigned pass; unpaid passes
qualify. Waitlist entries alone do not qualify. This is the participant selection
policy, not a grant to execute an arbitrary module or an administrator action.

Resolve returns a bounded participant summary, UTC instants and the event IANA
zone. Display times in that zone. At most 20 clarification choices are returned;
`more` signals further candidates. List returns the same qualified set in stable
UUID order, with `next` supplied as `after` on the next request. Separate pages do
not represent a frozen snapshot across requests.

Choose requires module, event ID, expected selection version, operation ID and a
matching idempotency key. Optional opaque run IDs are limited to 128 bytes and
scoped to the authenticated owner. A successful transaction saves the user choice,
optional run choice and original receipt together. Concurrent stale versions are
rejected. Eligibility is checked after locks using the current database clock.
Run choices survive bot restart and later choices in other runs. An unavailable
run choice falls back to the eligible user choice before hints or automatic selection.

Identical retries return the original receipt, including its original version;
they do not overwrite a later choice. Changing the request under the same operation
ID conflicts. Status is owner scoped. Missing status is unknown, not evidence of no
effect. Known business rejection is terminal; storage uncertainty remains retryable.
Replay never grants access to a now-ineligible event. Revoked accounts are denied.

The event summary includes optional registration pricing. Manifest `mvp-33`
accepts both fixed prices and the configured paired/distributed tier policy in
resolve, list and choose results. Status and reconcile use the same result schema.

Migration 00008 adds child tables for user choices, run choices and receipts.
Apply migrations before starting the candidate. Existing event and user rows are
not rewritten. Export these records before a deliberate rollback that drops them.

Acceptance on 2026-10-13: four independent review passes; the first found and later passes accepted a fix for unavailable run-choice fallback. Full Linux race suite
and HTTP/PostgreSQL tests passed. Real Zitadel tokens on the owned local stand
proved explicit priority, pagination, owner isolation, two independent run choices,
actual application restart, original receipts and fallback after disabling a module.
Ignored evidence: `.cache/shuttle-live-test/event-context-proof.json`.
Candidate image: `sha256:eb3606ebf0e12d014be766181b2cf17b7922530ce0d706c83de5802941061a0a`.
Native Loom join and choose broker writes and context script reads passed on the
isolated upstream working seal fixture described in
[fixture evidence](shuttle-loom-seal-fixture.md). This is not native Telegram UI
acceptance or proof that the upstream committed release contains the fix.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
