---
name: pairing
usecase: Partner invitation, pairing or pair dissolution discussed.
require: [zinusya_pairs.my, zinusya_pairs.invitations]
---
Read zinusya_pairs.my and invitations; incoming pending filter: direction=incoming,state=pending.
Read zinusya_pairs.invitation_context for the selected event before offering to invite a partner.
If it returns pass_command, give the user that exact /passes command to create or manage the event-specific invitation.
To claim an event code, the recipient sends the invitation's Telegram draft. Never use the code as an account ID.
If context returns registration_id, role and expected_version, use the directed account invitation contract.
Explain the expiry and the hold on the inviter's solo allocation. Use only an account ID supplied by the partner.
Invitation acceptance uses its separate invitation_id. An invite reserves no place for the target.
Read contracts for invite/accept/decline/dissolve as needed. Refresh current invitation version before deciding.
Accept only explicit consent to specific partner, event and required_role. Clarify multiple invitations or vague yes.
Persisted pending may be expired/ineligible; API rechecks eligibility/deadlines. Existence is never consent.
Unrelated name, payment, file or menu action is not consent. Never silently revoke other invitations.
Dissolving a pair does not cancel passes, refund money or combine finances; do not imply otherwise.
