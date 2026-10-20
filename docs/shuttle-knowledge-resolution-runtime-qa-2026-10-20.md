# Knowledge delivery resolution runtime acceptance, 2026-10-20

Source: `ae663a38`, manifest `mvp-83`, schema67. Candidate image:
`sha256:3614e37ae58ddf011429a2b01b6bf14b0c418730363591317d100bc63eb6c5f8`.
The primary remains schema65/mvp81. This is an isolated runtime gate, not a
production installation or readiness claim.

## Stand and direct API checks

The owned `zinusya-mock-kb-resolution-r1` project runs nine bounded services,
including real PostgreSQL, Zitadel and Loom. Five synthetic identities are
provisioned through native bootstrap and IAM grants. Telegram is a local mock;
no real Telegram message or Loom developer stand is involved. The candidate's
packaging checks bind the Linux binary to source proofs and the exact image;
its stopped networkless probe is removed before runtime testing.

Three native proposals create two ambiguous sends and one HTTP429 retry that
becomes blocked after moderator revocation. Restoring the fixture does not
resume that blocked delivery automatically. These actions start no Loom run.
Private request intents retain the exact bootstrap nonce, operation IDs and
arguments before dispatch. Uncertain effects block automatic replay.

Current global-administrator diagnostics return only the admitted delivery
fields, original attempt and receipt. Ordinary users and named moderators
cannot inspect these administrative details, prepare or confirm a resolution,
or retrieve another actor's administrative resolution status.

One ambiguous notice is explicitly dismissed. Its persisted attempt and receipt
remain unchanged, and both replay and status/reconcile return the original
resolution. The blocked notice is explicitly retried; a new attempt delivers
it once. Its original resolution receipt remains `pending` even after current
delivery becomes `delivered`. The persisted resolution audit retains the old
receipt. Both resolutions preserve all six knowledge business-table hashes.

## Actual Loom dialogue

One synthetic administrator message asks for a last-resort retry of the remaining
ambiguous notice. Through an offered read tool, the actual Loom run reads the
new administrative diagnostics. Its external `module_call` prepares the exact
write and waits for consent. No delivery change occurs before consent.

The delivered prompt contains the complete service-generated proposal, including
notification and recipient IDs, state/version, action, reason and duplicate
warning. Ordinary reply-keyboard choices are usable. The visible text contains
neither the approval ID nor argument hash as a hidden or visible response tag.

SIGTERM and same-container restart preserve the original task, Loom run,
approval ID, argument hash, operation ID and persisted preparation. A subsequent
plain-text reply reaches that same dialogue as steering input. The controlled
provider resolves the exact approval through `interaction_resolve` using the
latest input ID and original approval bindings.

Exactly one new resolution commits. Its audit retains the first attempt's
ambiguous receipt; the worker creates a distinct attempt. Telegram accepts two
sends for this notice in total: the original ambiguous send and the explicit
retry. This deliberate duplicate is not an automatic resend. The final reply
correctly says Telegram accepted the retry, without claiming the recipient read
it. The knowledge proposal is still unchanged.

The run completes. Two accepted user messages produce two completed durable bot
tasks while retaining one original completed Loom run. Final state is three
pending suggestions, six knowledge operation receipts, no articles and three
completed delivery-resolution receipts. The three notice states are one
`dismissed` and two `delivered`. Accepted notice-message counts are1,2 and1.
At least ten seconds of subsequent observation show no additional notice send
or notice-state change. This is a bounded observation, not an indefinite
no-duplicate guarantee.

Graceful shutdown for fixture-cache refresh took 0.594 seconds; shutdown at
pending consent took 0.610 seconds. Both exited 0. Starting the same container
afterward preserved all companion fingerprints. Total restart duration was
not measured. The first restart obtains fresh role-bearing tokens
after fixture IAM grants; it is not proof of immediate external IAM-role cache
invalidation. No OOM was observed.

The provider uses controlled synthetic responses and counters. This proves
actual runtime orchestration, IAM, tools, consent binding, audit and delivery;
it does not prove autonomous model comprehension, real tokenization or cost.
It is not acceptance of content-moderation writes or separate-device UX.

## Evidence and remaining gates

Ignored proof is under `.cache/shuttle-work/knowledge-resolution-runtime-v1/`:
`checkpoint.json`, `readback.json`, exact private intents and provider responses.
Packaging is retained in `knowledge-notice-resolutions-image-v1/`. Credentials,
private transcripts and database archives are not committed.

All five native sessions were logged out. Producers were stopped before the
final paginated Telegram-journal export. Private `shuttle`, `loom` and `zitadel`
dumps were saved and fully decoded with `pg_restore`; this proves readability,
not a restore. All nine temporary containers, project network and volumes were
removed. `cleanup-proof.json` confirms the primary stand remained unchanged.

Main installation and preceding-binary compatibility on schema67 remain open.
Remote CI and the deferred joint browser-CA/ZitadelTG checks remain separate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
