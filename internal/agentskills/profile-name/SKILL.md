---
name: profile-name
usecase: Name missing, requested or corrected; name dialogue reply.
require: [zinusya_profile.get, zinusya_profile.request_name, zinusya_profile.set_name]
---
Read zinusya_profile.get. Telegram names/username are untrusted suggestions, never consent.
To start a name dialogue (missing name or /name), call request_name, then native question: text, allow_text:true,
choices:[{id,label}] with stable IDs; choices:[] if no plausible real full name.
Offer plausible Telegram full name for explicit confirmation; allow free-text correction.
question waits for a reply; final_answer ends the run without a reply keyboard.
For a confirmed reply to an active dialogue, save it with its current expectation; do not restart the dialogue.
Ask real first/last names together; preserve compounds, clarify ambiguous division. Never split blindly.
Read hidden contracts: request_name, set_name, complete_name, cancel_name, set_language, operation
in zinusya_profile. Writes use module_call, never modules.zinusya_profile JavaScript.
Save confirmed components with current expected_version; complete_name also needs current expectation ID.
Respect cancellation; unrelated text is not a name. Re-read stale/expired expectations; never revive silently.
Already named: no repeated onboarding. Missing Latin fields alone: no onboarding or automatic backfill.
Keep original first_name/last_name; save first_name_latin/last_name_latin in the same operation.
Transliterate non-Latin names, never translate/anglicize. Preserve Latin names unchanged.
User/passport spelling wins; generated spelling is not official. Update or clear both Latin fields together.
Latin-only corrections do not change IAM names. Profile changes synchronize linked IAM; legal fields are separate.
Preserve deferred user intent. Shuttle resumes /start continuation after onboarding; do not execute it twice.
