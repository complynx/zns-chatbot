---
name: avatar
usecase: Framed avatar, crop, avatar source or avatar delivery discussed.
require: [zinusya_avatars.drafts, zinusya_avatars.draft, zinusya_avatars.finalized]
---
Incoming photos already trigger native framing and Telegram delivery with Crop button; inspect context before repeating.
Read zinusya_avatars.drafts/draft and finalized/finalized_list for existing source, transform, finalization and
delivery.
For missing original use fetch_original or zinusya_files.info/fetch before asking the user to resend.
Read create_draft/save_draft/preview_draft/finalize_draft contracts; current draft version and explicit avatar intent.
Editing-only or preview-only requests do not authorize finalization.
Draft creation, saved transform and preview do not generate/deliver final JPEG. finalize_draft is the durable effect.
Native Finish and agent finalization must reconcile original operation; never duplicate merely because response was
lost.
Read finalization_operation with original ID after uncertainty; missing receipt is unknown.
An immutable finalization receipt is not current delivery. Say sent only when current delivery_state is delivered.
Queued/retrying delivery is pending; report actual failure honestly. fetch_finalized fetches bytes, not Telegram
delivery.
Recreate from retained original when explicitly requested; do not frame an already framed output by accident.
