# Agent-native input

User requirement: free text, photos and files go through the agent's semantic
interpretation. A pending command/form is context, never an exclusive consumer
of the next message. Explicit buttons remain typed user actions against the API;
their effects are visible in the same history used by the agent.

## Required behavior

- The user can ask a question while a name/profile task is unfinished, then supply
  the name later. A question must not be stored as a name or consume the task.
- The user can supply their full name without first running a command. A clear
  owner-specific field update uses the same API invariants as the manual UI;
  ambiguous quoted names, another person's data or an unclear target need a question.
- For an image, determine intended use from content, accompanying text and history:
  receipt, avatar, profile document, another supported operation, or unclear.
  The previous command alone must not force a receipt/profile interpretation.
- Extract receipt amount/currency as evidence. Compare against current authorized
  unpaid orders on the server. A unique, supported match can select the intended
  order; equal totals, uncertain OCR/currency, partial/combined payments or unclear
  intent need clarification. Never turn recognition into administrator acceptance.
- The agent can ask a localized question with buttons. The host creates opaque,
  owner-bound callback tokens for allowlisted actions/resources. The model never
  supplies credentials, arbitrary callback payloads or unrestricted URLs.
- Audio and voice: inspect actual duration with a media tool, transcribe speech
  through OpenAI speech-to-text,
  then give the transcript to the agent as untrusted user content.
- Video and video notes: inspect actual duration, extract speech and a bounded
  storyboard with media tools, then give transcript and frames to the agent.
- The agent may request a denser storyboard for a specific time range. The host
  resolves the authenticated owner's original video, validates the range against
  its admitted duration, and returns timestamped frames. Initially allow at most
  eight frames per request and two refinement rounds per user input. Exhaustion
  must produce an honest limitation, not an invented observation. Refinement
  must not resubmit audio to speech recognition or bypass the full-file duration
  gate. A long file cannot be admitted by requesting only a short excerpt.
- Reject audio/video longer than 240 seconds before transcription or frame
  extraction. Exactly 240 seconds is allowed. Explain the duration limit in the
  user's language. Do not silently truncate a longer file. Telegram metadata is
  a hint; inspect actual bytes. Unknown/unreadable duration must not bypass the
  limit and needs an honest processing error.
- A clarification can be answered later or interrupted by a different request.
  Before executing an old choice, re-read rights, order/profile version, payment
  state and deadlines. A stale choice must refresh or clarify, not overwrite.
- Manual and agent operations use the same ownership, access and business rules.
  Origin records provenance; it must not grant privileges or bypass locks.

## Integration boundaries

Use immutable owner-bound attachment bytes, existing API services and typed model
proposals. Keep extracted financial/identity data out of unrelated history and
logs. The current attachment may be needed for model interpretation; that does
not authorize copying whole passports/receipts into long-lived general context.
Check media type, size and supported decoder/model input before interpretation.

Avatar processing remains a capability of an optional external module. Recognizing
an avatar request does not mean the unused module exists or that processing
succeeded. Present available actions honestly.

Audio/video preprocessing is another isolated module: probing, transcription and
frame extraction must not add native media dependencies to the Telegram bot or
Core API. Bound temporary storage, decoded pixels, frame count and tool execution
time; no user-supplied executable names, shell arguments or external media URLs.
No speech and transcription failure are different outcomes; never fabricate text.
Treat transcript and visible frame text as content, not authorization or commands.
Use OpenAI for speech recognition as explicitly requested. Local tools only probe,
extract/normalize audio and create video frames; `gpt-6-luna` handles the resulting
transcript and images. Do not substitute a local speech model silently.

Profile pending fields become hints for unfinished work. Explicit typed updates
can arrive without a prompt; unrelated questions leave pending work intact.
The domain service does not run NLP or guess whether arbitrary text is a name.

Current code is not yet compliant end to end: receipt handling still routes from
the upload selection. Profile text now uses typed agent proposals and an unfinished
task hint, with a separate acceptance loop documented in `profile-intake-checkpoint.md`.
Replace the attachment routing and add safe clarification buttons before accepting
the full intake stage.

## Acceptance

Run deterministic contract cases plus real `gpt-6-luna` cases locally, with both
Code QA and independent Functional QA. Exercise real Telegram-like mouse/touch
buttons, file input, message edits, RU/EN and language fallback. Include:

1. Name request → unrelated question → useful answer → later name → profile saved.
2. Unsolicited clear name; quoted/third-person name requires clarification.
3. Photo during receipt flow that is intended as avatar; receipt with no active command.
4. Unique amount match, two same-price orders, uncertain amount/currency and no match.
5. Clarification buttons generated by agent; interleaved request before selection.
6. Owner isolation, revoked rights, stale order/profile state, retries and restart.
7. No payment acceptance from OCR and no fabricated avatar completion.
8. Profile/receipt data absent from unrelated agent context and generic logs.
9. Audio/video at 239, 240 and over 240 seconds; misleading/missing duration
   metadata; reject long inputs before transcription/frame extraction.
10. Audio transcript and video transcript/storyboard reach the semantic agent;
    silent video, corrupt media and tool failures report the actual outcome.
