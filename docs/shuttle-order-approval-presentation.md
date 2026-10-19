# Complete order approval presentation

Actual embedded Telegram testing exposed incomplete order approval: the Loom
preparation description showed a total and line counts, but not item quantities,
unit/line amounts or required-charge names. A manual provider response included
the breakdown as Text alongside module_call; that text was not delivered before
the pending approval. This record makes no claim that this is a Loom defect.
The bot must present the exact proposed composition before consent.

The user sent a natural refusal asking to leave the saved choice. Real Loom
interaction_resolve rejected that exact approval. No order was created and the
draft was retained. The first observation and refusal are preserved under
`.cache/shuttle-live-test/orders-submit-real-qa/agent-first-presentation-finding.json`
and its checkpoint; they are not a passing agent-only scenario.

## Source change

For `zinusya_orders.submit`, the task service reads the immutable prepare receipt
using the original operation ID and proposed event. Lookup checks current
owner/event/module/assigned-pass access under the existing deployment scope. The
recipient Telegram identity must match the task's owner. Arguments are strictly
decoded and must equal the stored preparation; Loom's deadline cannot extend its
expiry. The current draft is not used to replace the proposed snapshot. An
unavailable/mismatched receipt returns before saving an approval presentation,
so the same durable task retries rather than falling back to incomplete text.

The rendered RU/EN message contains every item and required-charge line, offer
and item/supplement IDs, quantity, unit/line amount, total, cutoff and expiry in
the event zone, and the no-capacity/no-money consequence. Integer decimal
formatting preserves exact minor units. Literal fenced text protects labels;
there is no raw argument JSON for this specialized order presentation. Other
approval methods retain their existing complete-parameter rendering.

Full text uses the existing durable Telegram chunk/outbox path, with consent
keyboard only after the last chunk. It is not squeezed into Loom's 4000-byte
preparation description; that API contract and manifest revision remain unchanged.
The original Approval object still supplies all pending-action keys, protocol
callbacks and presentation context. Only display text is passed separately.
Returning from an intervening question therefore restores the same approval
keyboard without changing the action hash or deduplicating its new presentation.

Distinct item IDs remain visible even when names/prices match in the same offer.
The native review/history renderer now includes these IDs too; its existing
transaction/recovery behavior is unchanged. No dependency, migration, new grant
or proactive agent action was added. Schema and manifest remain 40/`mvp-40`.

## Verification and boundaries

A frozen Linux/race gate passed 26 integration roots/44 test events plus all
13 task roots/42 events: 39 roots/86 events, zero failures/skips/OOM, with real
disposable PostgreSQL and mock IAM/Loom/Telegram. The 875 frozen source inputs
remained unchanged. It includes new large same-name RU/EN quote rendering,
all 40 item identities and charge/total before the final keyboard, literal-label
handling, a changed current draft that must not reprice the original proposal,
missing-receipt retry/recovery, and approval → question → same approval keyboard
restoration. Existing conversational steering/protocol/keyboard, complete literal
parameter, native order/context and agent order API cases were included.

The Node VM gate passed all 65 UI behavior tests, zero failures/skips, including
a valid two-item catalog with identical names/prices that remains distinguishable
in native review. Host compile passed; final scoped task/integration lint had
zero issues. The initial compile used a pointer for a string profile field;
that test construction was corrected. Initial lint reported six issues, fixed
before the frozen final gate.

Independent correctness and quality reviews caught two implementation gaps:
mutating the displayed Approval would change its identity, and omitting line IDs
would hide distinct same-name selections. Both were fixed before the gate.
Tests now cover question-return and duplicate labels without embedding IDs into
those labels. A fresh final textual review found no actionable finding. Its
limits remain explicit: it ran no tests and did not prove runtime restart recovery
or every literal character independently.

Evidence is in `.cache/shuttle-work/order-approval-presentation-v1-qa/`:
JSONL/TAP, exact roots, immutable source manifests, terminal and cleanup receipts.
The exact bounded Go/PostgreSQL containers and dedicated volume were removed;
all ten main-service fingerprints remained unchanged through the gate. The Go
container had a 2GiB memory limit without swap, 2CPU, GOMEMLIMIT=384MiB
and test parallelism 2, and exited zero.

At the component gate, the live stand still ran the preceding `82c1…652a`
image. The corrected full release Dockerfile subsequently built image
`sha256:42a605470d00e189bc6a7096490b1e845ac340a3f229434b51f7a06db8ea1a40`
from 754 unchanged runtime build inputs in a 2GiB/no-swap/2CPU builder.
The dedicated builder/cache was removed after the successful build.

The independently reviewed image-only activation on `shuttle-live-test` passed.
It retained schema40, configuration, encryption key and existing business data,
checked a private backup archive, performed real IAM owner reads before ingress,
and restored the same poller. The eight companions, including Loom, kept their
fingerprints. Evidence is in `.cache/shuttle-live-test/order-approval-main-cutover/`.

Fresh real Telegram acceptance then displayed every soup/charge line and exact
25.68 BYN total with event-local deadlines. A new approval ID and a new free-text
response produced exactly one submitted order. The original refused approval
was not reused. Both mixed paths also passed: native saved choice to agent
approval, and agent saved choice to native explicit review/confirmation. See
`shuttle-orders-submission-real-qa-2026-10-17.md` for sources and limitations.

Controlled lost-response and clean bot restart acceptance subsequently passed;
see `shuttle-orders-response-loss-real-qa-2026-10-17.md` for the exact HTTP503
fault and limits. A later agent-first pending-proposal cross-channel replay passed; see
`shuttle-orders-shared-proposal-real-qa-2026-10-17.md`. A later native-first preparation and overlapping HTTP/API race also passed; see
`shuttle-orders-http-race-qa-2026-10-17.md` for fixture limits. Hard-kill acceptance
remains required. Manual-provider coverage does not establish
unattended model interpretation. Full
payment/capacity/meal stages and production readiness remain outside this
component. No commit, push, PR, external issue or production deployment was
performed; the index is empty.

## Frozen service context follow-up (2026-10-17)

The next source increment retains one service entry per selected offer: its ID,
EN/RU name and absolute UTC start/end. Both exact Telegram consent and native
review/order history render these saved values in the snapshot event timezone.
Catalog edits do not rewrite sold names or intervals. Missing service context in
historical orders remains absent; it is never reconstructed from today's catalog.
An unsubmitted legacy preparation fails closed on execution. A new preparation
operation creates updated terms requiring fresh consent; replay of the original
operation still returns its original receipt.

The final frozen component gate passed 29 integration roots/47 events plus
13 task roots/42 events: 42 roots/89 events, with no failures, skips or OOM.
All 876 frozen inputs remained unchanged. Real disposable PostgreSQL and Linux
race detection cover service deduplication, catalog-change/reopen recovery,
legacy consent renewal and historical omission, alongside the existing order,
conversation and approval cases. The UI gate passed 66 tests, including service
labels and distinct DST endpoint offsets across review/history and RU/EN changes.
Scoped db/task/integration lint reports zero issues. Two initial complexity
findings were simplified before the final gate; the earlier successful gate is
retained separately. Two independent initial textual reviews and a fresh final
review found no actionable findings. Reviews themselves executed no tests.

Final evidence: `.cache/shuttle-work/order-service-context-v2-qa/`; earlier
component evidence: `order-service-context-v1-qa/`. Exact temporary containers
and their PostgreSQL volume were removed; the ten live stand services retained
their fingerprints. This follow-up has not been built into or activated on the
live bot, so earlier real Telegram acceptance does not prove these new service
fields. Resource demands, evidence admission, capacity and financial effects
remain subsequent work under `shuttle-order-evidence-capacity-plan.md`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
