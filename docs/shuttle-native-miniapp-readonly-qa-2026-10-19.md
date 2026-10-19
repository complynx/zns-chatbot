# Real Telegram native commands and Mini App read-only QA — 2026-10-19

The installed local schema59 / `zinusya mvp-68` passes five native commands and
an actual Telegram Mini App login, read-only navigation and logout. This is a
bounded acceptance checkpoint, not complete native workflow or production acceptance.

## Native commands

The authorized existing account sent `/menu`, `/orders`, `/massage`, `/profile`
and `/registrations` once in the test-bot chat. All five durable tasks completed
through the structured route and all five replies have Telegram delivery receipts.
No task acquired an agent run or delegated session; Loom runs stayed at132.
Previous communication rows were preserved. Only the expected conversation
sequence increments and five intake/task/output additions changed, alongside
River bookkeeping. Business tables, users and schema were unchanged.

## Actual Mini App

The received Open saved selection button opened the actual Telegram Mini App.
It signed in without a user-facing Zitadel redirect. The configured public
`complynx.net/testbot/18382/app/` entry redirected to the local test override;
this does not establish local HTTPS CA installation or provider OIDC acceptance.

The form loaded an existing synthetic order, its confirmed payment and original
proof. Load this proof fetched the file into a browser blob. The massage section
loaded existing booked/cancelled history and displayed the Europe/Berlin DST
transition with the expected GMT+02 and GMT+01 offsets. No order, proof, payment,
booking, draft, name or registration mutation was submitted.

Avatar controls were present in the full application with no saved drafts.
Meals showed no accessible fixture event. Those empty states do not accept
avatar processing or meal editing. A direct native avatar command remains a
navigation gap; native avatar functionality already exists inside the Mini App.
Bare `/start` also has no structured handler in the installed version. Neither
unknown command was sent, because it would invoke the agent.

Sign out displayed the signed-out state. PostgreSQL independently confirmed the
new session is revoked. The modal was closed and the original test-bot chat
remained selected. All business and user rows stayed byte-equivalent under the
existing snapshot projection; no additional task or Loom run appeared.

## Retained discrepancies and tool limits

The first preservation assertion failed because login pruned24 expired sessions.
The installed59 archive reconstructed the exact pre-launch session-table hash.
Every deleted row belonged to this bot, had auth_date older than24 hours at the
new login, and had both deadlines expired. This matches the bounded cleanup in
`PruneMiniAppSessions`; the new revoked session is the only remaining row.
The failed blanket preservation assertion is not described as a pass.

Pointer input into the fractional iframe was rejected by the browser tool.
Documented selectOption and keyboard activation completed the read-only checks.
Direct screenshot file export returned EPERM; screenshots were displayed during
QA but no exported image artifact is claimed. Embedded blob download was
unsupported, and one ordinary download attempt timed out. Download completion
is unknown and was not retried or accepted. File retrieval into the browser is
accepted; an exported or visually inspected PDF is not.

One service output accidentally included a Telegram initData URL after logout.
It is not copied into reports or proof JSON. The session revocation was checked
in the database; this report does not claim that tool output was secret-free.

## Evidence and remaining work

Ignored local proofs under `native-launch-installed59-v1-qa` retain the before
snapshot, exact five-command readback, archive-backed session cleanup proof and
post-logout verification. Historical installation snapshots remain unchanged.
Eleven original services remain installed; this gate created no container.

Next: native bare-start and avatar entry points, then focused native/agent/mixed
workflow acceptance. Full form writes, unattended model behavior, exported proof
download, joint zitadeltg/browser activation and production acceptance remain
separate gates.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
