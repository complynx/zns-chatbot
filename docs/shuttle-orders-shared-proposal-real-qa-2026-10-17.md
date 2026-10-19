# Orders: actual shared pending proposal and canonical replay

This owned `shuttle-live-test` acceptance covers an agent-first pending proposal,
actual Mini App completion, and explicit resolution of the original agent action.
It is sequential. It does not establish concurrent execution or model reasoning.
The actual Telegram account8968148043 used only private chat6087685431 with
`@complynx_test_bot`. Real Telegram initData and Zitadel authenticated the Mini App;
Loom used the manual test provider through its real broker. No signed login
fixture substituted for the browser flow.

## Observed path

The synthetic agent-only event already had one submitted draft1 order and an
empty draft2. A new natural Telegram message347 asked to save two soup portions
and prepare an order, while waiting for confirmation. Real Loom read the current
catalog/draft, saved version3 and prepared an exact approval. No new order existed.
Message348 displayed soup `lunch/soup` x2 at12.34 BYN =24.68 BYN, required service
`lunch/service` x1 at1.00 BYN and total25.68 BYN. Cutoff/expiry used Europe/Berlin.

While that action was still waiting, the actual Mini App opened the same draft3
without saving it again. Its review displayed the same complete composition,
amount and expiry. Database read-back proved exactly one pending preparation for
that source draft: distinct agent/miniapp prepare operations referenced the same
proposal ID and had identical immutable preparation payloads. Neither channel
had submitted a new order at this point.

Explicit confirmation in the form created order
`e05c1280-e244-4d35-ac2e-9603303c03dd`, shown as submitted/unpaid. The draft became
empty version4; the previous order remained. The native submit receipt referenced
the original shared proposal. The agent task remained awaiting the user's reply.

The next actual free-text reply explicitly confirmed the same25.68 BYN order and
asked to return the result already created in the form without a second order.
The manual provider used `interaction_resolve` with that real steering input ID,
original approval ID and argument hash. The original agent operation then returned
the exact canonical OrderSubmissionResult already stored by the native operation,
including the same order ID, snapshot, submission time and cleared selection.
The QA helper parsed this real function-call output and compared the complete
payload before supplying a final natural-language response.

Database read-back found two submit receipts, trusted sources `miniapp` and
`agent`, referencing the same preparation/order and identical result. The agent
receipt used the original pending operation ID. Total event order count increased
by one from its baseline, not two; draft4 remained unchanged as decoded JSON
to the original cleared selection. The original older order was preserved.
The real Loom run completed, outbox delivery finished, and message351 acknowledged
that this was the same order with no second effect. The pending keyboard was
removed. Payment was not confirmed and no capacity was reserved.

## Review and evidence

Independent initial textual review found that checking only absence of an error
and inspecting the database would not prove what the agent actually received.
The helper was strengthened to require full equality of the actual resolve output
with the canonical payload. The response contract was checked against the exact
previous synthetic submit/resolve call IDs: initial submit returned blocked
pending_user_decision, whereas resolve carried the completed business result.
No prompt/token/private file contents were printed for that inspection.
A fresh complete v2 helper review found no actionable finding. Syntax checks
passed. No product source, image, migration, setting or dependency changed.

Evidence is `.cache/shuttle-live-test/orders-shared-proposal-real-qa/checkpoint.json`
and its exact provider-response request references. It stores immutable prepare
and submit receipts, actual agent-visible payload, run/task/operation identities,
UI observations, and fixed bot/poller/companion fingerprints. The standalone
helper is `.cache/shuttle-work/orders_shared_proposal_real_qa.py`; it writes its
own checkpoint and never overwrites the previous scenario's report functions.
Browser observations were checked directly; no exported screenshot is claimed.

The accepted schema40/image42a605…1a40 and original18382 app configuration were
unchanged throughout. All service fingerprints matched; no proxy, container or
volume was added. No production or developer Loom resources were changed. No commit, push, PR,
public issue/comment or message outside this test chat was made.

A later native-first shared preparation and genuinely overlapping HTTP/API race
passed; see `shuttle-orders-http-race-qa-2026-10-17.md`. It uses a local native-login
fixture, not Telegram UI or a Loom/model loop. Order proof intake, capacity,
payment and fulfillment remain required roadmap work. Existing component tests cover both shared
handoffs and concurrent aliases; they do not turn this sequential observation
into concurrent runtime evidence. Manual-provider execution does not establish
unattended interpretation of natural language.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
