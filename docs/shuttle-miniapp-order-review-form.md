# Native order payment reviewer form

Current owned runtime is schema49 / `mvp-56`; the
[latest owned image gate](shuttle-session-fallback-primary-qa-2026-10-18.md)
preserves the existing owner/reviewer forms, history and durable notices.
[Native proof/review](shuttle-order-proof-native-qa-2026-10-17.md),
[native-to-agent continuation](shuttle-order-review-late-real-qa-2026-10-18.md),
[owner mixed workflows](shuttle-orders-owner-real-qa-2026-10-18.md) and
[pure-agent review/cancellation](shuttle-order-reviewer-dialogue-qa-2026-10-18.md)
have scoped real Telegram/IAM/Loom proof with a manual provider.
The [isolated browser handoff](shuttle-order-browser-handoff-qa-2026-10-18.md)
now also passes the original blocked agent commit→native form→same-agent flow,
with distinct customer/ambassador identities and one financial review. It uses
mock Telegram and the actual shipped form/backend; the earlier real WebView input
barrier remains separate. Fault/outage, unattended model and D23 money-policy
questions remain open. Component versions below are historical milestones.

The Mini App includes a separate order-payment reviewer panel. It uses the
[review read API](shuttle-miniapp-order-review-reads.md) and
[review write API](shuttle-miniapp-order-review-writes.md); pass payment balances
and editable pass-credit amounts are not reused. Runtime identity remains zinusya.
The original implementation preceded activation; current owned runtime is
schema48 / `mvp-55` with the scoped gates linked above.

## Review and handoff

Choose an authorized event and an unresolved proof. The panel shows the payer,
immutable order total, current proof state and event-local file/deadline times.
The server derives review permissions. Proof loading uses the current Mini App
session and the evidence-scoped content route. The download link refers to a
revocable browser blob, not a Telegram locator or IAM token. The form requires
loading the exact proof before final confirmation; actual visual inspection is
still the reviewer's responsibility.

Preparing saves intent without confirming money. The complete immutable workspace
preparation description is shown as literal text, including the sold order terms,
amounts and reason. Final confirmation repeats that exact proposal and decision.
There is no editable credit amount. Pending claims can confirm or reject;
exhausted and needs_decision claims cannot confirm money through this panel, but
can be rejected or left for a later human decision. Rejection does not itself
transfer a refund. Cancelling a saved decision changes intent only; it does not
cancel the order or its proof claim.

The form opens agent-prepared workspaces. Edits disable confirmation until a new
revision is prepared or the current revision is reloaded. Completion by the agent
appears on reload and prevents another new confirmation. Current detail and the
original native request receipt are separate views; switching event or proof
clears the prior receipt and status. The exact-ID link
#order-review?event=UUID&evidence=UUID can open a reviewed or off-page proof.
Discovery and queues use bounded pages of at most20 entries.

## Recovery and presentation

Before each save, cancellation or confirmation, sessionStorage receives the exact
owner/target/phase/body/operation UUID; the controller verifies read-back before
sending. This recovery record has business intent, including the reason, but no
IAM token or Mini App bearer. The surrounding app manages its existing session
separately. Records are local to the browser session; this is not cross-device
recovery. Server operations and workspaces remain durable.

Uncertain requests block replacement. Reload reads the original operation;404 is
unknown and never evidence that the write failed. Only explicit retry resends the
same body and key. A completed original receipt is checked for source, operation,
kind and target before clearing recovery state. If the agent completed the shared
canonical decision but no native receipt exists, explicit same-key retry recovers
that canonical outcome. Invalid or unreadable startup recovery storage blocks new
writes. The original result does not assert that a newer workspace is unchanged.

Logout and target changes revoke proof URLs and clear file names/content. Epoch
checks suppress late replies after logout/owner changes. EN/RU language changes
update labels, state and receipts without rewriting the immutable description or
pending request. Event times use IANA timezones and retain DST offsets. User and
server strings are rendered with textContent, not HTML.

## Verification and remaining acceptance

All five Node UI suites passed103 behavioral tests;17 are new reviewer cases.
They include native preparation/confirmation/rejection/cancellation, both handoff
directions, unknown/lost replies, original receipt recovery without another POST,
canonical completion through the agent while native outcome is unknown, explicit
same-key retry, storage failures, logout/late responses, locale/DST, cursor paging,
and actual app session/file-read wiring. Source hashes matched before/after the
accepted Node run in ignored .cache/shuttle-work/order-review-ui-node-v5/.

The focused Linux/race API gate passed13 roots /46 root-and-subtest events with
923 frozen inputs unchanged, no failures/skips/OOM and all ten main-service
fingerprints unchanged. Scoped API lint passed with0 issues. The exact disposable
Go container was removed; no extra PostgreSQL instance was needed for this
asset-route change. Proof: .cache/shuttle-work/order-review-ui-go-v1-qa/.
Two additional Node cases were added after that Go gate; Go/SQL and product assets
were unchanged. The broader backend PostgreSQL proofs remain the separate read /
write API gates documented in their respective files.

Independent correctness and quality reviews were followed by fresh final reviews.
A verified receipt-isolation defect was fixed. Initial Node failures for storage
read blocking and a misleading confirmed-amount label are retained in v1 artifacts;
the independently reproduced receipt failure is retained in v2. Later accepted
runs did not overwrite those records.

Manual browser QA used actual app assets with a synthetic Telegram SDK and API,
not real Zitadel/Loom/Telegram services. It checked preparation without money,
proof-load gating, literal complete summary, EN/RU, DST offsets, one explicit
synthetic confirmation, disabled repeat confirmation, target change and logout.
The final v2 fixture additionally verified that switching target clears the old
status. Its local process and temporary tab were closed. Proofs and the earlier
Russian prepared-form screenshot are under ignored order-review-browser-v1-qa/
and order-review-browser-v2-qa/. The screenshot predates the status-only cleanup;
external PDF-viewer behavior and real attachment inspection were not accepted by
these synthetic PDF bytes. Other panels still have a separate known whole-app
language-refresh follow-up; this is not whole-app i18n acceptance.

[Reactive reviewer action history](shuttle-miniapp-order-review-context.md) now has
separate source verification. [Owner proof action history](shuttle-miniapp-order-proof-context.md)
also has separate source verification. At that original form milestone, notices,
activation and real-service/fault QA were pending. Current installed state and
scoped native/fresh mixed acceptance are linked at the top of this page; remaining
agent, fault and unattended-model gates do not imply production readiness.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
