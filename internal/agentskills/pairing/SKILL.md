---
name: pairing
usecase: Partner invitation, pairing or pair dissolution discussed.
require: [zinusya_pairs.my, zinusya_pairs.invitations]
---
Read zinusya_pairs.my and invitations; incoming pending filter: direction=incoming,state=pending.
Read invitation_context before inviting: own role/version/expiry; pending invite pauses own solo allocation.
Partner code is their account ID, supplied by that partner; never enumerate people. It is not authentication or consent.
Invitation acceptance uses its separate invitation_id. An invite reserves no place for the target.
Read contracts for invite/accept/decline/dissolve as needed. Refresh current invitation version before deciding.
Accept only explicit consent to specific partner, event and required_role. Clarify multiple invitations or vague yes.
Persisted pending may be expired/ineligible; API rechecks eligibility/deadlines. Existence is never consent.
Unrelated name, payment, file or menu action is not consent. Never silently revoke other invitations.
Dissolving a pair does not cancel passes, refund money or combine finances; do not imply otherwise.
