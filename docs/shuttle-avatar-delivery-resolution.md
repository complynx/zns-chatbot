# Avatar delivery resolution — administrator API

Schema59 / manifest mvp68 includes explicit, eventless administrator resolution
for saved-draft avatar intents through agent tools and the native administrator API.
The worker and finalization APIs are separate stages.

## Prepare, confirm and audit

The store requires a server-derived AvatarDeliveryAdminScope: deployment,
current user ID and Zitadel subject, plus GlobalAdmin. The HTTP caller
derives that flag through the existing administrator authorization policy, never
tool arguments. IAM verification uses the configured short-lived positive cache;
local account revocation is checked on every request. No per-request bypass of
that shared cache policy is introduced.
Every call, including a historical receipt read or exact replay, locks and checks
the current nonrevoked actor. No remote IAM I/O occurs inside the transaction.

Preparation accepts delivery ID, expected version, retry/dismiss action and a
reason of at most512 Unicode characters. An actor-scoped operation UUID binds
the normalized arguments. The persisted document includes the original delivery
snapshot, proposed action, a five-minute database-clock expiry and proposal UUID.
Preparing makes no delivery change. Retrying delivery_unknown includes an explicit
possible-duplicate warning: inspect the receipt and chat first; resend only as a
last resort.

Confirmation requires the same actor, operation, normalized arguments and proposal.
A non-replay must be unexpired and still match the delivery version. Only unknown,
failed or blocked delivery is repairable. Pending, retryable, live send_inflight,
delivered and dismissed states cannot be reset through this path.

Retry additionally checks that the current owner is not revoked, has a subject,
and still has the captured Telegram recipient ID. The worker checks fresh IAM
again before sending. Dismiss remains possible after recipient revocation or
rebinding. Neither action changes the artifact, recipient or draft.

The transaction locks operation, actor, canonical proposal, recipient owner, then delivery. Actor and
owner use SHARE locks; this fences identity changes without owner lock upgrades
when administrators repair one another's deliveries. The delivery row uses an
exclusive row lock and version CAS. A successful action increments its version,
clears the attempt/active receipt, and sets pending or dismissed. The immutable
resolution result retains the previous state, version, attempt UUID and receipt,
plus actor, reason, new state/version and database timestamp. Both changes commit
together. Old workers cannot finish the new generation.

Exact confirmation replay returns its original result before checking expiry,
current recipient eligibility or later delivery state; current administrator
access is still required. A receipt lookup is scoped to the same actor and
operation. Preparation replay returns its original document; an expired operation
cannot be repurposed with different arguments. Use a fresh operation only for a
new explicitly authorized action.

## Agent HTTP contract

The dynamic zinusya_administration module exposes avatar_delivery and
resolve_avatar_delivery only to a currently authorized zinusya.admin in the bot's
organization. Every endpoint repeats runtime authorization; discovery alone does
not grant access. Methods accept no caller-selected user, organization, event or
Telegram recipient. Inspection returns one deployment-scoped delivery and its
bounded receipt, without image bytes or credentials.

- POST /internal/agent/administration/avatar_delivery reads arguments.delivery_id.
- POST /internal/agent/administration/resolve_avatar_delivery/prepare binds the
  operation UUID and exact delivery/version/action/reason.
- POST /internal/agent/administration/resolve_avatar_delivery requires a matching
  idempotency_key and exact_parameters confirmation with the proposal_id.
- POST at the same path plus /status or /reconcile returns the actor-scoped
  completed resolution receipt for operation_id.

The existing Loom protocol-version1 envelopes are retained. Preparation returns
arguments, preconditions, description and expiry. The private stored snapshot is
not added as an extra preparation field; inspect the delivery separately. A
successful write returns succeeded plus its immutable resolution result. A stale,
missing or expired exact proposal returns a terminal rejected result; malformed
operation/key/confirmation is HTTP400. Missing diagnostic rows are HTTP404.

Status returns not_found while a resolution has no completed result, including a
prepared-only operation.
This does not mean the avatar document was not delivered.
Retry
or reconcile the same exact operation; never infer permission to generate another
avatar or resend under a new operation from that response.

## Storage and acceptance boundary

One additive table stores proposals and completed audit results. It references the
existing delivery; there is no event ID, Loom task, new queue, second JPEG or new
dependency. Its Down migration refuses to discard even prepared history. Export
alone does not make rollback safe: deliberate removal is a separate authorized
operation. Existing avatar rollback fixtures remove empty newer extensions first.

Focused behavioral tests cover explicit retry through the real River worker,
dismissal after revocation, stale attempts, authorization and deployment scope,
changed arguments, expiry, live-attempt rejection, concurrent exact confirmation,
recipient rebinding during approval and rollback evidence protection. These use
isolated real PostgreSQL and mock Telegram/IAM peers. Final command results and
source hashes belong to the ignored source gate, not this source description.

Independent correctness and quality reviewers inspect the textual diff.
It uses mock Telegram and a deterministic provider;
actual Telegram and unattended model acceptance remain separate.
Native/operator
repair UI remains open.
The [native Generate/send UI](shuttle-avatar-send-ui.md) now has isolated browser proof.
[Shared finalization APIs and native action metadata](shuttle-avatar-finalization-api.md)
are now implemented separately.

## Recovering an administrator proposal

The dynamic module also exposes two read-only methods:

- `avatar_delivery_proposal`: pass exactly one nonzero `arguments.operation_id`
  or `arguments.proposal_id`. The outer operation ID identifies the read call;
  the argument selects the original repair. Returns its canonical operation ID,
  proposal ID, original arguments, delivery snapshot, description and expiry.
  `state` is `pending`, `expired`, or `completed`, derived using the database
  clock. A completed proposal includes its immutable result, even after expiry
  or a later delivery change.
- `avatar_delivery_pending`: optional `arguments.limit` (0 or omitted defaults
  to20; maximum20) and exclusive UUID cursor `arguments.after`. Returns `items`
  and, when more matching rows were found, `next_cursor`. Pass that cursor as
  `after`. Pages may be shorter because proposal payloads have a48KiB aggregate
  budget, leaving room within the64KiB agent result limit. Ordering is by proposal
  UUID, not creation time. This is a live list: proposals can expire or complete
  between pages, and newly created earlier UUIDs require a new traversal.

Both reads require the current global administrator and are scoped to the exact
bot, organization and original actor. Another administrator cannot read that
actor's proposal. A pending list excludes expired and completed proposals; an
exact read can recover either. Reads do not extend expiry, supply consent,
change the delivery, or enqueue work. Existing `/status` and `/reconcile` remain
completed-result lookups: a merely prepared proposal still returns `not_found`.
[Native administrator HTTP continuation](shuttle-avatar-native-administration.md) is implemented separately; its browser UI and actual runtime handoff acceptance remain next.

## Continuing the same proposal under a new operation ID

Loom assigns its own operation IDs. To continue an existing proposal, call
`resolve_avatar_delivery/prepare` using the new outer operation ID and original
business arguments plus optional `arguments.resume_proposal_id`. Adoption requires
current global administrator access and the same bot, organization and original
actor. It verifies the exact normalized arguments, original proposal and database
expiry. An expired or completed proposal cannot be adopted. An already bound
operation cannot be redirected to another proposal or different arguments.

Preparation returns the original proposal ID, description, expiry and normalized
business arguments. The resume control is omitted from those returned arguments:
it is a transport locator, not a business change. The broker must still obtain
fresh exact human approval before confirmation. If a caller retains the optional
control in confirmation, it must match the confirmed proposal ID.

Aliases point to one canonical operation; they do not create another proposal,
result or deadline. Exact proposal reads accept an alias and return the canonical
operation ID. Pending discovery lists the canonical proposal once. Status and
reconciliation under either operation return the same immutable completed result;
prepared-only status remains not_found. Every confirmation locks the canonical
row before reading its result, then saves only the canonical result. Concurrent
confirmations through different aliases therefore return one result and apply
one delivery version change. Current actor authorization is still required on
historical replay. Recipient eligibility and expiry are checked for a new effect.

Migration58 adds only the alias table and a scoped foreign key to the existing
canonical operation. It preserves proposal and result data. Upgrade with one
application version at a time: stop the old app, migrate, start the new app.
The older binary does not understand aliases and must not overlap alias writers
or be rolled back onto a database containing them. The migration Down uses NOWAIT
and refuses any nonempty alias history, including completed aliases. Export and
separately authorize deliberate removal before reverting; no automatic discard
or cascade is provided. Empty Up/Down and preservation of existing proposals are
covered by the isolated PostgreSQL migration checks.
