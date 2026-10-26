---
name: pass-registration
usecase: Event registration, own pass or registration identity discussed.
require: [zinusya_registrations.me, zinusya_registrations.join]
---
Resolve event via zinusya_context.list for module registrations; read zinusya_registrations.me.
Read join contract before joining. Explain actual eligibility, roles, deadline and server result.
Waitlist, registration and assigned pass are distinct. Joining does not confirm payment or assign an arbitrary pass.
For legal identity, inspect optional zinusya_registration_details.get/set_legal_name/set_passport_number.
Profile name/transliteration is not legal identity. Never infer passport fields, expose private values or invent them.
Use current versions and explicit supplied identity. Absent optional module: capability unavailable; never bypass.
Administrative assignment requires its own authorized zinusya_administration contract and exact human confirmation.
