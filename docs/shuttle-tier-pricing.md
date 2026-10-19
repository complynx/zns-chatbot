# Tier pricing — operator-selected assignment

After the first native pair milestone, an independent architect and Go senior
agreed on operator-selected solo/pair assignment with participant-specific quotes.
The policy, owner quotes and immutable tier snapshots are implemented. Source
behavioral/race checks, independent reviews and the bounded native scenario in
[tier QA](shuttle-tier-qa-2026-10-14.md) passed. This does not implement allocation.

## Smallest complete path

An optional ordered `pricing` policy extends the registration rules JSON and
versioned event administration API. A tier has stable ID, amount, price_minor,
starts_at, promo and blocked_by_date. The policy explicitly selects paired
independent-role quotas or a distributed shared quota. Currency/scale remain event
terms. HTTP configuration supplies both boolean flags explicitly. Missing policy
keeps flat pricing; an empty policy is invalid. Tier prices are positive; flat
zero-price behavior remains unchanged. Deposit validation uses every tier price,
including stored-document reads; the unused flat price does not constrain a tier
deposit. IDs are unique lowercase slugs of at most 64 characters; at most 100 tiers
are accepted. Amount may be zero. Starts require absolute microsecond instants.

Use one pure resolver with explicit current database instant, assigned counts,
explicit tier usage and candidate roles. Include every assigned owner pass in
pricing statistics irrespective of payment or role-balancing exclusions. Prior
passes with no tier snapshot still contribute to participant totals. Current
registrations remain assigned when money is confirmed; do not require a paid state.

Existing solo and pair preparation endpoints persist structured owner quotes in
their immutable proposal JSON. Show each owner's tier, price and deposit terms in
the exact approval description. Paired participants can have different prices.
Do not accept an agent-supplied arbitrary amount or add an implicit date bypass.

At confirmation, resolve quotes again after recipient locks and the final database
clock check under the existing event transaction. Reject a changed tier/price/
deposit quote before any pass write. Event version alone cannot detect usage or
time-driven quote changes. Both paired quotes must be checked before either pass
is created. The existing pass writer takes the resolved owner price and freezes
that owner's deposit terms. A stale quote needs a new preparation and approval.

Migration 00018 adds an immutable pass tier side table with stable configured ID and original
ordinal. Keep existing pass rows and fixed-price receipts intact; use stable IDs
for explicit tier usage after edits. Preserve existing request argument digests.
Check committed receipt replay before applying new quote validation. A legacy
preparation without quotes may execute only when the event still has flat pricing.
Tier-enabled assignment without preparation is rejected. Agent output contracts
include the optional frozen tier so Loom can accept committed results and status.

## Legacy behavior to preserve

The source is zns-chatbot/plugins/passes.py:2677–3005 and 3547–3651, with behavior
examples in tests/test_passes_assignment_framework.py. Port these selection rules:

- Paired quotas use amount divided by two, rounded down, independently per role.
  Distributed quotas use the full amount across both roles.
- Effective usage is the greater of explicit usage and implied usage. Implied
  usage clamps participants minus prior configured capacities to zero/current
  capacity. Never clamp explicit usage after a permitted pair overshoot.
- Start at the last configured tier whose start is at or before the current
  instant; use the first tier when none has started. Configured order is retained,
  including promo and zero-capacity tiers; do not sort by date.
- A future blocked_by_date tier stops scanning before promo or zero-capacity
  skipping. A nonblocked future tier can become eligible through prior capacity.
- Solo assignment may use promo tiers. Pair assignment skips promo tiers. In
  paired mode, resolve each role independently, allowing different tier prices.
- A distributed pair uses the same tier for both owners. It may exceed that
  tier's amount by exactly one only when one place remains and a later positive
  nonpromo tier exists before a future date barrier.
- No eligible tier means no assignment. Do not fall back to flat pricing or the
  last tier. Earlier unsold capacity affects displayed availability only, not
  selection capacity.

All tier starts are absolute instants interpreted/displayed with the event's IANA
zone under the existing event time contract. The resolver receives one instant;
it must not read a global now_msk or host timezone.

## Proof and rollout gates

Behavioral tests must cover split-price paired passes and owner deposit snapshots;
distributed boundary overshoot and blocked successors; stale usage/date quotes
with zero partial writes; competing assignments; and original receipt replay after
policy changes. Retain flat config, pass and old preparation compatibility tests.
Then run independent correctness/quality review loops and real IAM/Loom exact
approval/restart acceptance on the owned stand before accepting this slice.

Apply the additive migration before the new binary. To return to a binary with
the old strict rules decoder, first remove the optional tier policy using a normal
versioned configuration edit. Keep tier history for binary rollback; export before
destructive schema rollback. Older binaries cannot enforce tier semantics.

Automatic allocation, queue-order choices, balance policy, double-solo atomicity,
administrative date overrides and cancellation/refunds are separate later slices.
No new service, provider or generic operation framework is needed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
