# Native payment review HTTP adapter

Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

## Routes and decisions

All paths below start with /app/payment-reviews/. GET requests require the current
ZinusyaSession authorization; POST also requires the configured Origin and JSON.
The static events/ prefix keeps event routes distinct from operations/ in ServeMux.

| Method | Suffix | Result |
| --- | --- | --- |
| GET | events?after=UUID | Current reviewer-only event choices, at most20 plus next cursor |
| GET | events/{event}/evidence?after=UUID | At most20 authorized evidence/history records and possible next cursor |
| GET | events/{event}/evidence/{evidence} | Evidence, current balance and this actor's latest workspace or null |
| GET | events/{event}/evidence/{evidence}/content | Bounded authorized proof attachment |
| GET | operations/{operation} | Original authorized receipt, from either transport |
| POST | workspaces | Save exact pending review; no money credited |
| POST | cancellations | Cancel a pending workspace; no pass/payment cancellation |
| POST | confirmations | Explicitly confirm the exact review and canonical proposal |

Event discovery includes event ambassadors/administrators and a verified global
zinusya.admin, including historical events with the registrations module enabled.
An ordinary pass owner does not gain reviewer access, even for their own evidence.
Evidence pagination may return a cursor when the page has exactly20 entries and
the following page is empty. Metadata reads can span separate authorized store
reads; write CAS/proposal checks remain authoritative for financial decisions.

### Payer display

The owner is derived from the evidence, never a browser-supplied target user.
The display name uses the chosen bot profile name, then Telegram first/last name,
limited to256 characters; it is empty when neither exists.
Clients can show the
UUID in that case.
This is a display label, not a verified legal identity.
No
passport, private registration fields, Telegram username or IAM identity is added.
Current reviewer authority and session are checked in the lookup transaction and
again before the complete detail is returned.
List and write contracts are unchanged.

A save body has operation_id (UUID), expected_revision and review. Review contains
event_id, evidence_id, expected_version, decision, amount_minor and reason. Numeric
zero must be explicit for the revision/balance version and rejection amount; missing
version/money fields are not silently treated as approval of zero. Amounts are
integer minor units, with frozen currency/scale supplied by the balance/proposal.

A cancellation body has operation_id, workspace_id and revision. Confirmation has
operation_id, canonical_proposal_id, confirmed:true and arguments containing the
workspace_id, revision and full review. Missing consent, changed money/proposal,
stale/cancelled intent and invalid decision are rejected. Unknown JSON fields and
trailing values are rejected; bodies are bounded by the existing64KiB Mini App limit.

Keep the same operation and exact body after an uncertain response. Successful
writes return bare native domain results; operation status returns its receipt.
A prepare/cancel receipt is not a financial success. An unknown operation returns
404, which means unknown, not proof that a financial effect did not occur. A lost
successful confirmation replays the original result and never credits money twice.

## Authorization and proof IO

The browser cannot supply actor, organization, source or administrator flags.
The server obtains the owner's token, verifies its principal and deployment actor,
then rereads the session before constructing scope. Existing account/profile
handlers keep their prior error-only principal contract through a small wrapper.
IAM verification retains the configured positive-cache policy; this is not an
uncached introspection guarantee. Local event authority and session are checked
in store transactions, including known receipt recovery.

Proof download checks the reviewer role/session, resolves the evidence-file
association, performs bounded Telegram IO without DB locks, then invokes IAM
verification again, rereads the association and checks the current event role and
session before returning bytes. The association must still identify the same file.
A logout or local role revocation during source IO blocks disclosure. Existing
20MiB file read limits apply. Responses use attachment, private/no-store, nosniff
and sandbox CSP. Missing/oversized source files fail without returning proof bytes.
General file endpoints remain owner-only; payment review grants no general access.

Native save/cancel/confirm use existing atomic session guards and completed native
call receipts. They do not start an unsolicited agent run. Their redacted action projection is included only in the next explicit agent turn.
No IAM bearer token or raw Telegram initData is returned by these payment routes.
