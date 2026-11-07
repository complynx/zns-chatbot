# Native solo pass assignment

This stage
extends the source checkpoint from 2026-10-19.
It does not establish complete registration/pass/payment parity.

## Permission and user flow

The Mini App shows the solo assignment workspace only to an active account
with the current global `zinusya.admin` role. Event administrator rights alone
do not authorize solo assignment. Pair assignment has its separate policy.
The session and actor come from server authentication, never browser arguments.

Select an event and a named Telegram participant from paged lists. There is no
implicit first selection. Paired or held waiting-list registrations are omitted
from solo targets; execution also checks pair membership and current capacity.
The list exposes display names and Telegram IDs, not legal or passport fields.
Refresh events remains available on the first page and after a failed event read.
UUID pagination does not express waiting-list priority.

Preparing a proposal reserves no place and records no payment. Review its exact
recipient, role, event, price, tier, deposit terms, payment window, and expiry,
then explicitly check consent and confirm. The server rechecks current authority,
session, proposal identity, argument digest, versions, details, closing time,
and capacity before committing the pass and canonical receipt together.

## HTTP contract

All routes use the existing Mini App session, origin, JSON limits, and no-store
responses under `/app/pass-assignments/`:

- `GET events?after=UUID`: `{events, next?}`, at most 20 deployment events.
- `GET events/{event}/registrations?after=UUID`: `{registrations, next?}`;
  entries contain `registration`, `telegram_user_id`, and `display_name`.
- `GET proposals?after=UUID`: actor-owned pending proposals, at most 20.
- `GET recent`: at most 20 native proposal groups ordered by their latest
  native action time, including completed receipts and expired proposals.
  This recovers recent operations after browser storage is lost.
- `GET proposals/{proposal}`: original terms and observed pending, expired,
  or completed state; a completed result is a historical receipt.
- `GET operations/{operation}`: the same actor-owned view by opaque operation
  key, including a transport alias. Encode the key as one URL path segment.
- `POST prepare`: `operation_id`, the four exact `arguments` fields, and optional
  `resume_proposal_id`. No confirmation is accepted at preparation.
- `POST confirm`: the same operation and arguments, `proposal_id`, and
  `confirmed: true`. A resume field cannot replace confirmation.

The four argument fields are `event_id`, `registration_id`,
`expected_event_version`, and `expected_version`. Operation IDs remain text of
1 to 128 bytes; proposal IDs are UUIDs. A foreign actor cannot inspect or adopt
another actor's proposal. Current global permission is checked before the
operation and before returning its result.

## Recovery and mixed interfaces

The browser journals only owner/operation/proposal identifiers, original
argument identifiers and versions, and recovery phase. It saves these before
POST. It stores no names, prices, credentials, or proposal description. A storage
failure blocks new writes. Reloading a surviving browser context recovers the
same journal. After closing that context, use Recent form operations to discover
the latest 20 native proposal groups, or the paged pending list for outstanding
proposals. This bounded history does not promise permanent browser storage or
an exhaustive completed-operation archive. An unknown confirmation with a
surviving local journal cannot be discarded to start another assignment.

Any preparation-only browser record can be closed locally, including one with
an unknown preparation response: preparation cannot assign a pass or reserve a
place. Closing is not server cancellation; a saved proposal can still be found
in the pending list and continued with fresh explicit consent. An unresolved
confirmation is different: its original operation must be checked before the
local record can be closed. Storage failures continue to block changes.

An agent can adopt a native proposal with `resume_proposal_id`, preserving its
original terms, expiry, and canonical receipt. Adoption supplies no consent;
fresh exact approval remains required. The form can discover an agent-created
proposal and confirm its original opaque operation. Replays and transport aliases
return the same committed business result after restart or proposal expiry.
A changed request cannot reinterpret a reserved operation key.

New preparations freeze the event IANA timezone with their original description.
The form displays expiry and payment deadlines in that zone. Older proposals
without a stored zone use explicitly labelled UTC; their description is retained.

## Transaction and reactive history

Transport namespace locks precede canonical namespace locks where required,
then the scoped event, actor SHARE lock, session UPDATE lock, canonical proposal,
and recipient locks. Session validation does not upgrade the actor to UPDATE.
Local IAM invalidation/expiry fences and database session time are rechecked
after blocking waits and before commit. No IAM network call runs under DB locks.

Schema 61 journals native preparation and confirmation locators in the same
transaction, including successful recovery of an earlier result. Repeated
transport calls do not rewrite the original journal timestamp. The next active
user request receives at most five actor-owned historical locators strictly
before its task cutoff, after current global administrator authorization.
History is untrusted context, not a new request, current state, or approval;
no native action starts a proactive Loom run.

Schema 60 owns transport aliases. Both new migrations protect populated data
from destructive downgrade; retain expanded schema for binary rollback. A
schema downgrade requires deliberate export and reconciliation first.
