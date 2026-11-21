---
name: pass-registration
usecase: Event registration, own pass or registration identity discussed.
require: [zinusya_registrations.me, zinusya_registrations.join]
---
Resolve event via zinusya_context.list for module registrations; read zinusya_registrations.me.
Offer /passes <slug> for the selected event, using its UUID when it has no slug. This opens native ticket selection.
For an automatic sale with promo tiers, explain that selecting a ticket there can start time-limited solo priority.
Let the user choose whether to select that ticket.
An explicitly requested ordinary solo registration can use zinusya_registrations.join with the chosen event and role.
Read its contract first. Joining alone does not select a promo ticket. Explain eligibility, roles, deadline and result.
Waitlist, registration and assigned pass are distinct. Joining does not confirm payment or assign an arbitrary pass.
Use zinusya_registration_details.readiness to find required fields for this event.
For a legal name, passport number or issuing country explicitly supplied by the owner, use the corresponding
set_legal_name, set_passport_number or set_passport_country method.
For a missing passport photo, direct the user through /passes for this event to the protected registration form.
Never request the image in chat.
Profile name/transliteration is not legal identity. Never infer passport fields, expose private values or invent them.
Use current versions and explicit supplied identity. Absent optional module: capability unavailable; never bypass.
Administrative assignment requires its own authorized zinusya_administration contract and exact human confirmation.

For a protected participant ZIP, use zinusya_registrations.send_identity_export on the organizer's request.
Resolve the event and obtain exact confirmation. The bot sends the ZIP to the organizer's private Telegram chat.
Report the delivery status. If delivery is uncertain, ask the organizer to check their chat.
