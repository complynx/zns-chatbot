# Incoming pair invitation context

The reactive prompt now includes a bounded snapshot of the authenticated user's
incoming `pending` invitation records, alongside profile/name and payment context.
This does not start a run or treat another action as consent.

The slice reuses the existing `PairInvitations` read and
`zinusya_pairs.invitations` refresh tool. No endpoint, migration or new waiting
framework is introduced. Owner, current subject, bot and organization constrain
the read; administrative rights do not add another person's invitations.

The snapshot contains `observed_at`, at most five invitations, `has_more` and
optional `next`. The cursor is the last included invitation UUID. Refresh with
`direction=incoming`, `state=pending`; follow `next` as `after`. UUID order does
not mean priority. Display names are capped at 256 Unicode code points; encoded
items have a 32 KiB page budget. Names and event descriptions are untrusted data.

These are persisted pending records, not an eligibility list. An elapsed record
may remain pending until the existing expiry process resolves it; it is included
with `expired_at_observation=true` when expiry is at or before observation. Reads
do not expire or otherwise modify invitations. Disabled/closed events, revoked
inviters or changed registrations can also make acceptance unavailable. No full
copy of acceptance predicates is maintained in the prompt builder.

Event start/end and invitation expiry are represented in the named event IANA
timezone, using the offset applicable to each instant. The snapshot carries the
existing invitation version, inviter public display name/account code and required
opposite dance role. It adds no payment or private profile directory for the inviter.

The prompt requires explicit consent to the specific partner, event and role.
Several invitations or an ambiguous “yes” require clarification. An unrelated
name, menu action, file or payment is not consent. Before a decision, refresh the
invitation; the write API still checks current version, deadlines and eligibility.
An invitation does not authorize silent revocation of another invitation, pass
assignment or financial changes.

New run creation and textual answer/steering preparation include the snapshot
before the outgoing payload is checkpointed. An uncertain retry reuses that exact
payload; a new textual input refreshes context. A failed read prevents dispatch of
an incomplete prompt. Individual profile, payment and invitation reads are separate
snapshots, not one globally atomic snapshot; every write rechecks current state.

## Accepted owned-stand boundary

Architecture and Go consultations preceded implementation. Focused race checks
passed, including expiry equality/DST representation, bounded cursor/JSON output,
multiple invitations with an independent name request, saved retry payloads and
fresh answer/steering inputs. Independent correctness, quality and privacy/protocol
reviews found no remaining issues; a fresh final review after lint fixes was also
clean. The final complete explicit-package race run passed (integration 234.928 s),
and the application linter reported zero issues. The source still uses manifest
`mvp-21` and schema 22 because no API or schema contract changed.

Owned native acceptance used two synthetic invitations to the current Telegram
owner, together with existing name and payment expectations. The actual run
received all three contexts and refreshed incoming invitations through a real
Loom script read. An ambiguous “yes” produced a clarification question without
creating a pair. The owner then requested an unrelated joke; that same run
continued without accepting/declining either invitation or changing name/payment
state. After a bot restart, both invitations remained readable and no pair had
been created. Only the authorized current test-bot chat was used.

Mock-peer checks prove context delivery and absence of automatic business effects.
The native provider was the manual Codex mailbox, so native acceptance proves
real-service plumbing for these choices, not an unattended model's consent accuracy.
Existing explicit accept/decline API behavior was preserved rather than rewritten.
Production and browser CA/zitadeltg acceptance remain separate gates.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
