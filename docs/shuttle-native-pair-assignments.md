# Native pair pass assignment

This extends the
[canonical pair foundation](shuttle-native-pair-assignment-foundation.md).
## Permission and shared operation

An active Mini App owner needs current global `zinusya.admin` permission or
administrator authority for the specific event. Solo assignment retains its
global-only policy. Session, actor and permissions are derived server-side.
Native APIs, agent APIs and both continuation directions use the same six
business arguments, canonical proposal, immutable quotes and one two-pass receipt.
Preparation and adoption reserve no capacity and supply no human consent.
Confirmation requires the exact proposal and arguments plus explicit approval.

Event, actor and session barriers precede the proposal row lock. The actor lock
stays SHARE; there is no lock upgrade for native access. Session validity and the
local IAM invalidation/expiry fence are rechecked after waits and before commit,
including when replaying an existing receipt. Network IAM calls stay outside
these transactions. Existing membership, capacity, versions, legal requirements,
module availability, quote and closing-time checks govern every new effect.

Completed receipts remain readable after proposal expiry, sales closing or
module disablement under current authority. They describe the original result;
current pair and pass state must be read separately. An active account can inspect
only its own proposals, even when it can administer another actor's event.

## Form and recovery

Choose an event and a pair explicitly. Both participant display names and
Telegram IDs are visible, followed by separate role prices, tiers, deposits and
deadlines. Stored proposal timezone governs display; older proposals without it
use explicitly labelled UTC. Canonical descriptions remain verbatim.

Pending/recent lists and exact recovery responses include role-bound participant
identities from the original pair memberships. This works after the pair leaves
the candidate queue and after a fresh browser launch. Display names are current
profile metadata, separate from the frozen terms. Legal and passport fields are
not included. Revoked participant profiles are not read; their Telegram ID remains
an account locator for historical inspection.

The browser persists owner and operation/proposal/concurrency identifiers before
POST. It does not store names, prices, credentials or consent. Lost replies use
read-only lookup of the same operation. An unresolved confirmation retains its
operation and blocks a replacement. Retrying confirmation needs fresh explicit
consent. Closing a preparation is local only and does not cancel the server
proposal. Owner changes fence late responses and clear the presentation.

Corrupt, oversized or unreadable local recovery identifiers block mutation but
keep authorized server history and receipt inspection available. Storage failure
never authorizes a new preparation, adoption or confirmation. Read-only inspection
preserves the existing journal, including an unknown confirmation, and does not
enable consent until valid identifiers can be persisted.

## HTTP and history

All routes use the existing Mini App bearer, origin checks, strict bounded JSON
and no-store responses under `/app/pair-assignments/`:

- `GET events?after=UUID` and `GET events/{event}/candidates?after=UUID`.
- `GET proposals?after=UUID`, `GET proposals/{proposal}` and
  `GET operations/{operation}`. Operation keys are opaque single encoded segments.
- `GET recent`: up to twenty canonical groups from the owner's native history.
- `POST prepare`: `operation_id`, six-field `arguments`, optional
  `resume_proposal_id`. Confirmation fields are forbidden even if null/false.
- `POST confirm`: `operation_id`, exact `arguments`, nonzero `proposal_id`,
  `confirmed: true`. A resume field is forbidden even if null.

Paged reads contain at most twenty rows. Current event access is filtered before
the limit. Cross-event lists acquire no event locks after the actor/session
barrier; exact reads use one pool lease at a time and retain current authority.
Authentication failure returns 401, permission loss 403, unavailable actor-owned
objects 404, changed terms/capacity/eligibility 409, and invalid input 400.

Migration 63 adds immutable native prepare/confirm locators in the same transaction
as proposal adoption or the original receipt. Retries preserve their first
timestamp and original snapshot versions. Populated history blocks Down until
explicit export/reconciliation; an empty Down preserves canonical proposals.

The next user-triggered agent request receives at most five currently authorized
history locators recorded strictly before its task creation. These are historical
untrusted metadata, not live state, consent or a proactive instruction. A later
alias or result cannot enter an earlier task's context. The agent can read the
original proposal/result and adopt an unexpired proposal under a new operation
key, then ask for fresh exact approval. Native writes do not create agent tasks.

## Participant identity reads

Agent candidates and actor-owned exact/pending proposal reads include participant display identities. An event-only administrator needs no unrelated global user lookup to identify the pair. Current event authority scopes every read; revoked candidates are filtered before pagination. The shared helper reads no revoked profile name and changes no frozen description, command hash or quote. Both recognizable names and Telegram IDs must be shown before approval.

## Approval presentation follow-up

The task delivery path now resolves the actor-owned canonical proposal through
its original operation or adoption alias. It checks current event authority,
original arguments and expiry before displaying both current participant names,
Telegram/account IDs and the unchanged original description. Names are literal
text, separate from frozen business terms. Configured deposit amount/share,
frozen final top-up deadline, IANA timezone and approval expiry remain visible.
Model-authored captions cannot replace these service-verified details.

If the form has already completed this same operation, the prompt explicitly
asks to reconcile its historical result. It does not suggest assigning again.
Other approval methods retain their existing routing.

## Frozen display terms

New preparations retain structured display terms for the original participants, price, configured deposit minimum amount/share, effective required deposit and deadlines. Historical proposals without structured terms retain the original-description fallback. Presentation does not change the proposal, expiry, action hash or confirmation arguments.
