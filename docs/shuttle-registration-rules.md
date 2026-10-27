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
