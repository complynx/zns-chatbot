# Order reviewer dialogue and cancellation — 2026-10-18

Later acceptance: the [isolated browser gate](shuttle-order-browser-handoff-qa-2026-10-18.md)
passes the complete blocked agent→native→same-agent handoff with distinct synthetic
customer/reviewer identities and real IAM/Loom/backend at schema49/mvp-56. It does
not turn the historical Telegram WebView attempt below into a pass.

Status: PASS for pure-agent confirmation and free-text cancellation of a review
intent. Agent-to-native confirmation is NOT accepted: the native form loaded,
but the browser instrument rejected its Load proof click. Real Telegram current
account and `@complynx_test_bot`, real IAM/Loom/PostgreSQL, synthetic events and
manual provider responses only. No real money or unattended-model acceptance.
Runtime: schema48 / `mvp-55`.

These runs continue the two existing owner claims from
[owner QA](shuttle-orders-owner-real-qa-2026-10-18.md). No order, evidence or file
was submitted again. Existing event-scoped ambassador authority was used; no new
IAM grant or permission change was made.

## Pure-agent confirmation

The Telegram request asked to review the existing two-soup order for 25.68 BYN,
prepare a decision and ask separately before confirming. Actual Loom broker reads
returned the pending evidence, absent workspace and immutable quote. The proof
was fetched through `zinusya_order_payments.fetch_for_review` using the evidence
ID. Loom imported a 1555-byte PDF with source/hash/expiry metadata. The manual
provider selected the known synthetic proof; this is not autonomous PDF analysis.

Saving the intention created one revision1 workspace and no review, money or
allocation change. Preparing review_commit displayed the exact Russian summary:
quote, reason, event-local dates/deadline, original decision expiry and two held
units. The separate free-text answer authorized only that exact pending action.
The persisted Loom input belongs to this run and contains the expected affirmative
text; the human review timestamp follows its receipt.

One canonical review for 2568 BYN minor units records the current ambassador.
The existing claim changes only pending/version1 to confirmed/version2, and its
two allocated units change held to occupied. The original order is byte-for-field
unchanged. There is one completed workspace, one canonical operation, one save
and one completed agent commit receipt. No fulfillment or refund is implied.
Two tasks completed, three task messages and one new confirmed-payment notice
were delivered. The final reply and notice were observed in Telegram.

Run: `82b21393-4cab-48cc-ac40-0a7dcdbe8704`.
Workspace: `e433be34-eada-4f71-a547-c77e0d84aba4`.

## Native attempt and cancellation

A second explicit Telegram request prepared the other existing claim for native
confirmation. Actual broker reads imported its distinct 1565-byte proof and saved
one revision1 workspace. The agent then prepared the exact dialogue approval.
No human review or money effect existed at this point.

The existing evidence notice opened the actual Telegram Mini App. Screenshots
showed the selected synthetic event, correct order, pending claim, 25.68 BYN,
the agent's saved reason, original expiry and disabled Confirm saved decision
before proof loading. No preparation was regenerated. The accessible/DOM tree
exposed only the underlying Telegram page rather than these WebView controls.
A screenshot-based Load proof click returned
`Cannot safely inspect fractional iframe input coordinates`. This was not
bypassed, and no native commit occurred. Escape closed the Mini App.

A new free-text message requested stopping only the prepared review and dialogue
wait. interaction_resolve rejected the dialogue action; cancel_review cancelled
only that workspace. The full evidence and held allocation rows remained exact,
with no review/canonical operation and no confirmed or rejected payment notice.
The original order, PDF reference and two held units remain. The save and cancel
receipts completed; the unused commit receipt remains uncompleted for audit.
Two tasks completed and three task messages delivered. The final cancellation
reply was observed in Telegram. Retrying native confirmation requires a new
revision and fresh consent; this cancelled intent must remain preserved.

Run: `eca86365-1454-47fc-b98f-8ca062825ac5`.
Workspace: `81d52859-58aa-44d3-8bd8-e8b8ddb6187b`.

## Evidence and remaining gates

The finite manual-provider driver passed independent review. The read-only final
gate checks complete claim/order/allocation preservation, original preparation,
canonical operation uniqueness, persisted consent/cancellation inputs, task/delivery/notices and the unchanged
ten-container baseline after image55 activation. Its first review identified a
missing persisted-consent text assertion; the corrected gate reads that actual
input and checks its ID, run, steering kind, exact affirmative text and timestamp.
A later review added the same persisted-input and timestamp checks for cancellation.
The first runtime gate also exposed an incorrect QA assumption: the old owner
record held four order columns, not a full row. Its retained failure was resolved
by comparing the complete order-table fingerprint with the pre-review activation
snapshot, while preserving the original partial-record check. No business action
was replayed. No new containers were needed. No unfinished bot task remains;
no service OOM.

Ignored evidence: `.cache/shuttle-work/order-reviewer-real-v1-qa/`, including the
actual tool results, before-consent rows, native UI barrier, cancellation context,
after-effect rows and final checkpoint. Screenshots were inspected in the browser
tool; no local screenshot file is claimed. Historical owner and activation
reports retain their original pending states and versions.

Next: finish native confirmation once the browser exposes usable controls, while
continuing process/outage hardening. Unattended model behavior, full massage
parity, full live-controller rollback and production acceptance remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
