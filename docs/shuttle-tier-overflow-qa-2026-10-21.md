# Tier overflow after date advancement, 2026-10-21

## Defect and correction

In distributed pricing, an expired tier's unsold capacity could hide overflow
from a later tier. For example, configure an expired tier with 100 places, a
current tier with one place, and a future eligible tier with one place. With no
existing passes, a pair can use the current price for both owners, consuming the
current and next places. Previously another solo could still receive the next
price: cumulative counts subtracted the expired tier's unused 100 places.

The resolver now carries explicit excess from each tier into the following
tier, independently of cumulative implied usage. It takes the larger of those
two usage calculations, preserving legacy/removed-tier counts without adding
the same overflow twice. Role-specific paired pricing retains separate counts.
The policy remains bounded to 100 tiers; no schema or API changes are required.

## Verification

- Before the fix, both new public pricing scenarios fail: the resolver allows
  an extra solo when it should return `ErrCapacity`. They cover a successor
  filled by overflow and a successor with exactly one remaining solo place.
- The registrations, registration-details and events package tests pass after
  the correction.
- The isolated Linux/PostgreSQL gate passes 21 roots / 38 cases with the race
  detector: ten registration-domain roots and eleven integration roots, with
  no failures or skips. The new database scenario actually assigns the pair,
  rejects another pass preparation and verifies the two original registrations
  and passes remain unchanged. Existing tier replay, date changes, stale quotes,
  deposits and competing approvals are included in the selection.
- After that run, only line wrapping of the new integration-test struct literal
  changed; comparison against the frozen source confirms whitespace-only
  differences. Final local lint covers registrations and integration packages
  and reports zero issues. Earlier lint formatting failures remain retained.
- Two independent reviews of the focused correction report no actionable
  findings; the second includes the database regression scenario.

## Scope and custody

This is domain and actual-PostgreSQL evidence with mocked upstream peers, not a
Telegram, real-IAM or model-driven registration acceptance run. Full remote CI
for this correction and installation on the owned primary remain separate gates.
The primary still runs schema69/mvp-89; production was not changed.

The disposable PostgreSQL and runner containers, their network and data volume
were removed after terminal success, log capture and full decoding of a retained
database dump. No OOM was observed, PostgreSQL stopped with exit code zero, and
the primary's domain data, queue, Loom state and runtime epochs were preserved.
Evidence is retained under `.cache/shuttle-work/pricing-overflow-v1/`, including
the original red log, Linux JSON events, lint attempts and retirement checkpoint.

The earlier Mini App-only candidate image was built and its builder removed,
but its installation is held so that a combined candidate can include this fix.
