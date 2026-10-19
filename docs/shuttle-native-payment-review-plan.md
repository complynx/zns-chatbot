# Native payment review: implementation plan

Current acceptance: [four real Telegram happy paths](shuttle-payment-review-real-qa-2026-10-16.md)
pass with real IAM/Loom and a manual provider. Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

Status: store, agent API and native session/HTTP transactions pass component checks;
the [form](shuttle-miniapp-payment-review-form.md) now passes source component
checks. The [native entry and reactive context](shuttle-payment-review-entry.md)
pass component checks; full workflow acceptance remains open. The
[local launch rollout](shuttle-miniapp-local-launch-qa-2026-10-16.md) installed the
store/API/HTTP components on owned schema39. The later display-name projection is
component-verified and installed with the reviewer form in the current owned candidate. Existing agent payment review and money rules remain authoritative.
The [transaction-core extraction](shuttle-payment-review-tx-qa-2026-10-16.md) passed
focused before/after checks. The native workflow itself remains open.
The [typed workspace store](shuttle-payment-review-workspaces.md) is implemented;
its [agent adapter](shuttle-payment-review-workspace-api.md) now passes component checks.
The [native session store](shuttle-miniapp-payment-review-store.md) and
[HTTP/proof adapter](shuttle-miniapp-payment-review-http.md) are now component-verified.
This plan follows architecture and senior Go consultations. Orders/meals and massage
remain separate workflows; their latest agreed rules are recorded in the
[infrastructure priorities](shuttle-infrastructure-priorities.md).

## User workflow

A native /payment_reviews entry opens a Mini App. Current event ambassadors and event
administrators, or a verified global zinusya.admin, choose an event and pending evidence.
They inspect the proof, reported amount, current balance, frozen currency/scale and local
event deadline. They explicitly enter the amount to credit or a rejection reason, prepare
an exact decision, then confirm or cancel. Preparation alone credits nothing. Display an
existing decision as history. Preserve the existing durable owner notification.

The same workflow must work wholly through the agent, wholly through the form, and in
both directions while a decision is pending. A saved intention is not human approval.
No user-facing text tags or new proactive agent invocation are required.

Persist a bounded native action journal for preparation, edit, cancellation and
confirmation in the same transaction as each corresponding workspace change.
Before the next user-triggered new run or continuation, supply redacted metadata
(action, event/workspace IDs, revision and outcome), filtered by current authority.
Do not include proof bytes or free-text reasons in this context. The agent can read
current permitted workspace details when needed. These records are context, never
consent, and must not proactively start or resume Loom.

## Shared decision and transport identity

Loom generates its own operation ID. It cannot adopt a native operation ID. Keep an
immutable canonical payment operation C and proposal P for each exact prepared decision.
A typed review workspace binds actor, evidence, revision, normalized parameters, C, P
and expiry. Editing or cancelling invalidates pending transport intents for the old
revision; it must not silently move an old approval to new parameters.

Bind each outer call L immutably to that workspace revision and exact parameters. The
agent review_commit preparation must show the actual amount, decision, reason and domain
preconditions under exact_parameters. Return L in the transport envelope and C in the
result. Native confirmation uses the same C/P. A late agent call after native completion
returns the original receipt without a second financial effect. Historical replay must
not reset a later workspace revision. An operation-only status read takes L, checks
current access and returns its original result; a missing receipt is unknown.

## Transaction and access boundary

First extract a private transaction-aware helper from ReviewPaymentEvidence. Preserve
the public interface, validation, canonical operation/event/actor lock order, proposal
check, original-receipt lookup before expiry, ledger and owner notification behavior.
Run the same payment/notification/authority behavioral tests before and after extraction.
Do not introduce a generic workflow or transaction framework.

The subsequent commit transaction must include canonical review, ledger version,
notification intent, canonical receipt, workspace completion, outer receipt and native
action journal. Validate the immutable outer intent before effect; return an authorized
original receipt before testing pending-workspace revision/expiry for a new effect.
Keep operation -> event -> actor -> target lock ordering consistent. Never hold locks
during remote file IO. Mini App session validity must be checked inside the mutation
transaction; authentication and role discovery outside it are insufficient.

### Native transaction implementation decisions

Extract private transaction helpers for workspace save, cancel and commit; keep
public agent methods as wrappers. Share the exact financial mutation rather than
copy it. Native source and session credentials come from server-side wrappers,
never from browser arguments. Existing completed call rows with source miniapp
serve as the native action journal; no extra workflow or journal table is needed.

The payment session guard runs after canonical operation/event/actor locks and
before every receipt return or write. Keep actor FOR SHARE and then session
FOR UPDATE; never call lockMiniAppBearer here, because its owner FOR UPDATE would
upgrade the existing actor lock. Concurrent reviews for different events by the
same actor could deadlock on that upgrade. Recheck the authenticated owner,
organization, subject, bearer, key generation, active state, local revocation and
database-clock expiry while those locks remain held. Logout takes owner UPDATE
before session UPDATE and therefore serializes with payment writes. Native status
reads also require an in-transaction session check before historical disclosure.

Native explicit confirmation sends the displayed immutable workspace revision,
full normalized review arguments, canonical proposal P and stable operation L.
Create its outer alias and apply the canonical decision in one transaction. Check
exact parameters/proposal and current authorization even on replay. Recover a
matching canonical receipt before pending-state/expiry gates: calling the existing
agent prepare-commit method unchanged would reject a decision already completed
through another interface. Generate a separate outer proposal only for the
transport binding; it does not replace the displayed canonical proposal. Save
native completion, canonical effect and receipt atomically.

Implementation must prove same-actor/different-event concurrency, logout against
commit, revoked/expired session replay denial, native/agent receipt recovery,
exact-argument/proposal mismatch and rollback before outer receipt completion.
These transaction decisions are now implemented and component-verified; browser
behavior and journal context now pass component checks; full native workflow
acceptance remains open.

Proof content uses an evidence-specific session endpoint. Check current event authority
before fetching and again after remote IO, before returning bytes. Keep general file
access owner-only. Enforce the existing bounded content limit, private/no-store and safe
content headers. Use explicit PDF download where rendering is unavailable. Browser
sessions must never expose IAM bearer tokens. A notification link opens its associated
evidence; clicking it never confirms money.

## Required acceptance

- Native-only operation with Loom unavailable; agent-only and both pending handoff paths.
- Pending prepare/edit/cancel and completion reach the next explicit agent turn; no
  unsolicited run/resume, no hidden approval and no context after authority revocation.
- Corrected credited amount, deposit/partial payment, rejection with reason and late cases.
- Concurrent reviewers/tabs and native/agent commits produce one ledger effect and notice.
- Stale, cancelled or edited proposals cannot approve replacement parameters.
- Expiry blocks a new effect; replay after expiry/later activity preserves the old receipt.
- Lost replies and restart recover via operation-only status without new operation IDs.
- A failure after ledger write but before workspace/outer receipt rolls everything back.
- Event/global role revocation during proof IO and before commit blocks disclosure/effect.
- Unavailable or oversized proof, forwarded notice, foreign evidence and session logout.
- Exact money formatting and ru/en event-local times with IANA timezone information.

Use focused behavioral/critical-path tests and fresh independent review loops. Then run
real Telegram transport against the owned stand with real IAM/Loom, clearly separating
manual-provider and direct-form proof from trusted Telegram WebView and model acceptance.
Do not change production, the Loom developer stand, or publish while D04 is held.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
