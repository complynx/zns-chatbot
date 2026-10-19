# Owned order submission activation and native acceptance

The owned `shuttle-live-test` stand runs schema40/manifest `mvp-40`, current image
`sha256:42a605470d00e189bc6a7096490b1e845ac340a3f229434b51f7a06db8ea1a40`.
Native-only submission/cancellation passed in the actual embedded Telegram Web
Mini App with Loom stopped. Agent-only and both mixed real paths subsequently
passed after correcting incomplete approval presentation; their first failure
and explicit refusal remain preserved below. Agent paths used real Loom with a
manual provider. The submitted-unpaid four-path acceptance is complete; order
proof/capacity/payment/fulfillment and broader roadmap remain incomplete. No
production-readiness or unattended-model claim is made.

The original schema40 activation below used image `82c1…652a`; the later corrected
image-only activation and additional recovery/concurrency probes are distinguished
in the follow-up and linked records.

## Activation

A fresh independent textual activation review found no actionable finding.
The procedure checked the exact accepted build, UI and cloned migration receipts,
then stopped the existing poller, drained tasks and stopped only the bot. It
saved a pre-migration custom-format dump and checked its archive listing. The
exact release image migrated39 to40. Existing table content hashes were retained;
the three new preparation/order/receipt tables were empty. On startup only
allowed River leadership/queue bookkeeping changed; queue creation, names,
metadata and pause state were checked independently against the backup.

The bot was recreated with its existing configuration, environment, Mini App key,
mounts, network and security/resource settings. The other eight services,
including owned Loom, retained their fingerprints. Fresh real Zitadel token
exchange then read the authorized owner's profile, redacted registration-detail
status, entitled orders events and empty submitted-order list. The owner probe
was bound to this exact candidate instance, subject, activation ID and key hash.
The same poller container was restored and completed fresh polling cycles.
The live HTML and orders JavaScript bytes matched the accepted sources and were
served with `Cache-Control: no-store`.

Evidence: `.cache/shuttle-live-test/orders-submit-main-cutover/` contains the
activation checkpoint, private backup and owner probe. These are local ignored
artifacts; credentials and private dump contents are not published.

## Actual native-only path

Four isolated synthetic assigned-unpaid event/catalog fixtures were created
through the existing administrative/owner APIs, only on our stand. They target
native-only, agent-only, native-to-agent and agent-to-native acceptance. No new
identity or privilege was granted. The actual Telegram user is `8968148043` in
private bot chat `6087685431` (`@complynx_test_bot`). No other chat was used.

With only owned Loom stopped, the actual `/orders` message completed through the
structured command route and returned the Mini App button. The embedded app
opened through the existing testbot redirect with actual Telegram initData and
real IAM authentication. No locally signed login fixture was used for this path.

In the native-only event, browser controls saved two portions, reviewed the
exact quote and explicitly confirmed it: soup 2 × 12.34 BYN = 24.68 BYN, required
per-offer service charge 1.00 BYN, total 25.68 BYN. The event uses Europe/Berlin;
service, cutoff, expiry and submitted times rendered in that zone with GMT+2.
The draft advanced to version2 and became empty. “My orders” showed the retained
snapshot as submitted/unpaid with no capacity or payment claim. The explicit
cancel button then changed the same order to cancelled version2.

Database verification found exactly one order, immutable submitted receipt at
version1 with total2568 minor BYN, and one each prepare/submit/cancel receipt,
all trusted source `miniapp`. The only new task was the completed structured
`/orders` command with an empty run ID. Loom run/input counts did not increase.
The form was switched to RU, where labels/items/charge/total rendered in Russian,
then the original EN bot language was restored.

The bot was cleanly stopped and started in the same container while Loom stayed
down. The same Mini App session then reloaded the cancelled order and unchanged
25.68 BYN snapshot. The same Loom container/image was finally started and became
ready; the other companion fingerprints were unchanged. This proves native
history/session survival across this clean application restart. It does not
prove an in-flight ambiguous commit/lost-reply under real Telegram; that has
component coverage and still needs the intended runtime recovery scenario.

The first database probe wrongly expected `run_id` to be null. Schema1 defines
it as non-null text with an empty-string default. The probe was corrected to
that actual representation; no product code or message was changed/repeated.
The original helper and failure explanation are retained, not rewritten as a
successful first attempt.

Evidence: `.cache/shuttle-live-test/orders-submit-real-qa/` contains fixture,
native receipt/source, no-agent-run, restart and restored-Loom checkpoints plus
the initial probe failure. Browser rendering was observed directly, including a
review screenshot; no exported screenshot file is claimed by this record.

## Agent-only and mixed follow-up

The first actual agent-only attempt showed incomplete approval composition.
Its natural refusal completed with no order and the draft retained. This failed
presentation remains in the checkpoint; it is not counted as accepted behavior.
The source fix and component evidence are in `shuttle-order-approval-presentation.md`.

The corrected image `42a605…1a40` then passed an independently reviewed image-only
activation on the same owned schema40 stand. The exact current image is recorded
in that presentation document and the activation checkpoint. The encryption key,
configuration and business data were preserved; eight companions, including Loom,
were unchanged. The same ingress poller was restored after real IAM owner reads.

A fresh natural Telegram continuation read the retained agent-only version1 draft
through real Loom and prepared a new operation/approval ID. Message332 displayed
soup `lunch/soup` ×2 at12.34 BYN =24.68 BYN, required service `lunch/service` ×1
at1.00 BYN, total25.68 BYN, and cutoff/expiry in Europe/Berlin. Approve/Reject
were ordinary reply-keyboard labels without tags; raw argument JSON was absent.
No order existed before consent. A new free-text reply resolved the exact
pending input/approval/hash through `interaction_resolve`. One submitted order
was verified and message335 showed the result; the keyboard disappeared.
This fresh path completes the agent-only scenario begun before the presentation
fix. It is not an uninterrupted successful first attempt.

For native-to-agent, the actual Mini App saved two soup portions as draft1 with
no new Telegram task or Loom run. The next explicit natural message contained
the trusted saved-action metadata; the broker read the same draft and prepared
it. Message337 showed the complete25.68 BYN offer. Clicking the reply keyboard
sent the plain text `Approve`; real Loom resolved that exact pending action.
There was one submitted order and exactly prepare/submit receipts, both source
`agent`. Message340 showed completion and the keyboard was removed.

For agent-to-native, an explicit natural message asked only to save two portions.
Real Loom read the catalog/draft and saved version1, with zero preparations or
orders. Message342 stated that only a draft had been saved. The actual Mini App
then opened the same version1, without saving it again, and showed both line IDs,
quantities, unit/line prices, charge,25.68 BYN total and event-local deadlines.
Clicking `Confirm this order` created order
`3097b53a-9cd3-4431-97bb-ad63f0bec9c9`, shown as submitted/unpaid. The draft was
cleared once to version2. Exactly prepare/submit receipts had source `miniapp`;
no new task or Loom run occurred during native completion.

Per-phase provenance is immutable in `agent_only_fixed`, `native_to_agent` and
`agent_to_native`. Top-level `active_task`/`active_run` are the mutable current
cursor. A fresh database read bound the completed phases to three distinct runs:
agent-only `b56c03eb-1cfe-4cd3-955b-b893771cc102` (task sequence144),
native-to-agent `d69a4d59-845c-4d7f-ad07-141a50a22378` (sequence146), and
agent-to-native `f80fd485-69a1-428e-8d06-66e78aa57f2b` (sequence148).
The exact request/task/run mapping is retained in `four-path-provenance.json`.

Two additional QA helper predicates initially failed before sending any decision:
one compared raw steering text against runtime text enriched with marked profile
context; another forgot that a real approval reply has its own persisted task.
They were corrected to compare the exact user-text prefix before the documented
context boundary and the latest known steering task/run respectively. Original
helpers and failure records were retained. No message was repeated to fix them,
and no product source changed during these real scenarios.

Evidence remains in `.cache/shuttle-live-test/orders-submit-real-qa/`: original
native acceptance/restart, failed first presentation/refusal, fresh approval and
all four path outcomes. Helpers are `orders_full_approval_real_qa.py` and
`orders_mixed_real_qa.py` under `.cache/shuttle-work/`. The provider is a manual
mailbox: actual Telegram, IAM, Loom and broker/storage behavior were exercised;
unattended model interpretation is not established by these results. Browser
observations were checked directly, with no exported screenshot file claimed.

## Remaining acceptance

Controlled lost-response and clean bot restart acceptance now passed; see
`shuttle-orders-response-loss-real-qa-2026-10-17.md`. A later
agent-first pending proposal shared with the Mini App and original agent canonical
replay passed; see `shuttle-orders-shared-proposal-real-qa-2026-10-17.md`.
That proves this one sequential pending-action path in addition to the earlier
shared-draft paths. A later native-first preparation and overlapping real HTTP/API race also passed;
see `shuttle-orders-http-race-qa-2026-10-17.md`. That probe uses a local native-login
fixture and direct agent API; it is not a concurrent Telegram UI/model run. Component
coverage is not substituted for that remaining runtime acceptance.
Full order proof/capacity/payment review and meal fulfillment, interactive
zitadeltg/CA and the remaining roadmap remain required work. Preserve pending
requests and failures; do not resend solely on an observation timeout.

No temporary build/clone/migrator containers remain. The ten normal services are
running. The preceding image and private backup remain available. No commit,
push, PR, production deployment or external issue/comment was performed; the Git
index is empty.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
