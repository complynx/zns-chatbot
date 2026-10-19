# Event registration rules — implementation prerequisite

Event document PUT/GET accepts optional registration configuration. A successful
change commits the event version, rules and original operation receipt together.
The same key returns its original receipt after later edits or restart. Existing
receipts without registration remain unchanged. An omitted/null registration
field clears current configuration; it does not rewrite receipts.

Rules explicitly specify leader/follower capacities, price_minor, currency,
currency_scale, closes_at and payment_window_seconds. Price uses exact integer
units with the declared decimal scale, not floating point or currency conversion.
Integers above 2^53-1 are rejected to preserve JavaScript/Loom round trips.
Currency is a three-letter uppercase code; this does not validate an ISO registry
or infer its scale. Scale is explicit and bounded 0..4. A zero price is allowed;
no payment confirmation or ledger entry is implied.

Optional pricing configures ordered tiers with explicit paired/distributed quotas.
See [tier pricing](shuttle-tier-pricing.md) for date/promo behavior, owner quote
confirmation and immutable pass provenance. A configured tier policy supplies the
assignment price; price_minor remains required for flat-configuration compatibility.
The deposit minimum is validated against every applicable tier price. Old strict
decoders cannot read pricing: remove the policy through a versioned edit before
returning to such a binary. Retain the additive tier history table for rollback.

Closing requires an absolute RFC3339 instant at microsecond precision, normalized
to UTC, no later than event end. Display uses the event's IANA timezone. The
assignment calculation caps its configured payment window at sales closing;
passing that deadline does not automatically cancel the pass or forfeit money.

Migration 00005 adds a child rules table and leaves the parent event table shape
unchanged. Older event binaries using SELECT * / RETURNING * still work and retain
these rules while ignoring them. New reads join both tables in one SQL snapshot;
writes share the parent event lock/transaction, avoiding mixed rule/version reads.
Keep the child table for binary rollback; down migration drops its configuration.
Apply explicit migrations before running the new binary. No production migration
or old payment import is performed.

Syntactically valid but invalid stored rule JSON causes 503, rather than silently
assuming a free pass or default capacity. An authorized full replacement can
repair or clear it using the correct event version.

This configuration is the prerequisite for implemented solo registration, pass
assignment, assigned-pass module eligibility and agent write adapters. Configuration
by itself grants no user access. See [solo registration](shuttle-solo-registration.md),
[agent assignment](shuttle-agent-assignments.md) and [event context](shuttle-event-context.md).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk

## Verification — 2026-10-13

Two fresh independent review passes were completed. The first found missing/null
zero-valid fields could decode as free/zero capacity; strict JSON decoding now
requires every declared field and rejects unknown fields. Behavioral HTTP checks
cover missing/null price, capacities and scale, persisted corrupt objects, repair,
restart, immutable replay, and legacy receipts with no rules. Affected production
packages and the full integration suite passed on Linux with the race detector;
Golden golangci-lint reports zero issues.

Actual native Zitadel administrator requests created rules, preserved integer
price and normalized a Minsk deadline, and replayed one original receipt. The
actual previous release binary then read and updated the same parent event under
the expanded schema. Restoring the candidate retained the rules and the original
receipt. Messaging counts stayed 2 users / 8 tasks / 8 outbox rows throughout.
Candidate image:
sha256:5e3839c064b8cf49e5b936c1949e1805754059ea989199f16f8b47d108155b8b.
The ignored registration-rules-proof.json retains exact fixture IDs/results.

Older binaries ignore these rules and cannot enforce their new semantics. Edits
to legacy event dates can make a retained closing instant invalid; the new binary
fails closed until the configuration is repaired. Rollback does not establish
registration/pass feature compatibility. At this historical checkpoint, those
operations were not implemented yet.

Current owner and administrator adapters, exact confirmation and context resolution
are implemented. Native broker acceptance uses the [isolated operation-seal fixture](shuttle-loom-seal-fixture.md);
this does not certify the upstream issue 3 image fixed. See the accepted
[pair flow](shuttle-pair-qa-2026-10-14.md) and [tier assignment](shuttle-tier-qa-2026-10-14.md)
for current verification boundaries.
