# Archived native name reply implementation

Historical snapshot before the conversational port. The implicit name write on a
Telegram reply reference described below was removed from current source. The
older owned image remains separate from acceptance of the new adapter. See the
[current name-entry guide](../shuttle-telegram-name-entry.md).

# Direct Telegram name entry

The working tree adds /name and /name followed by the full name. The former
presents a plain-text name request using Telegram ForceReply, not an agent turn.
It keeps the shared profile name expectation and its expiry. The owner can reply
to that specific bot message after a restart, navigate other menus, use the Mini
App profile form or use the explicit command. Ordinary free text is still agent
input; an expectation alone never reclassifies it as an application name answer.
Choices and approvals from agents still require the separate reply-keyboard stage.

## Binding and recovery

A direct task persists its exact profile mutation, operation ID and expected
version before applying it. Existing profile operations provide immutable replay
receipts. A /name prompt stores the presented expectation ID, version and expiry
in the durable task checkpoint. The known Telegram message_id receipt binds a
reply only within the same owner/conversation and after that prompt's sequence.
The outbox must be delivered and carry the ForceReply payload. Expired, completed
or superseded expectations and stale versions cannot overwrite a newer profile.

If the matching bot prompt is still send_inflight, routing waits for its receipt.
If the receipt is delivery_unknown, the quoted bot text and bot sender identify
only a manual recovery path: no write is authorized by that text. The bot asks for
/name plus the full name or a fresh /name request; it does not send the ambiguous
name answer to an active Loom run. Recovery applies only to a quote matching our
stored name prompt, not to arbitrary replies in the chat. Existing uncertain-send
records are retained and never blindly resent.

Intermediate saves for structured tasks preserve the active agent task as well
as the routing watermark. Successful name writes and expectation completion use
the same profile version/receipt logic as the agent and Mini App. Navigation
history also includes completed manual profile requests that have a matching
profile operation receipt, with the resulting version. It is bounded untrusted
context on the next genuine user-triggered agent input, not proactive dispatch,
consent or proof of delivery. Mini App write-action history still needs its own
transactional context integration; this note does not close that requirement.

No schema, dependency or additional worker is introduced. ForceReply is a typed
sendMessage variant, including decoding for saved outbox replay. An older binary
cannot process these new routes/checkpoints/keyboards: stop ingress and reconcile
accepted tasks and undelivered outbox before an old-image rollback.

## Verification state

The combined profile/name image is built and running on the owned stand. Native
/name request, quoted answer, restart with Loom offline, stale-answer refusal and
explicit name restoration passed. Both mixed directions and the full agent name
path also passed through real IAM/Loom with a manually driven provider. This does
not establish unattended model behavior, trusted Mini App form acceptance or full
registration/event selection. See the [native QA report](../shuttle-profile-name-native-qa-2026-10-16.md).

Initial review found a receipt-key mismatch and ambiguous routing after a lost
send receipt; both were verified and fixed. Root review also fixed intermediate
structured saves replacing an active agent. The first Linux attempt retained a
name-answer timeout and an old exact-profile-text assertion. A second attempt
retained a duplicate update ID in the expanded active-agent test, then that test
was corrected. Failed containers were terminal/noOOM and removed by exact identity.
The fresh Linux/race run passed83 root tests /166 root-and-subtest pass events:
all32 db,10 task and17 Telegram roots, plus24 selected real-PG integration roots.
There were no failures or skips. This was not a full integration-suite rerun.
Its source and ten main-container fingerprints stayed unchanged; the terminal
Go and PostgreSQL containers exited0 without OOM and were removed by verified
identity. A fresh reviewer found no actionable findings. Strict lint then required
combining two local variable declarations; the exact one-line, declaration-only
delta is retained separately. Final targeted race verification after that delta
passed all four selected critical integration roots with no skips or failures;
its source and main containers stayed unchanged and its terminal Go/PG containers
were removed0/noOOM. Evidence is under telegram-name-postlint-qa/. Strict lint
reports zero issues.
The83-root receipt remains under ignored .cache/shuttle-work/telegram-name-final-qa/;
it is not presented as a broad rerun after the declaration change. Earlier failed
attempts remain under telegram-name-qa/ and telegram-name-retry-qa/ for diagnosis.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
