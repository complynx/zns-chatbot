# Protected Mini App and agent continuation acceptance, 2026-10-16

Scope: the owned `shuttle-live-test` environment, bot image
`sha256:7645802278be3867dd84179b3daad04847bf0db9a67d458ac95118c7ba4ee3de`,
Shuttle schema37 and Loom schema4. The conversational upstream source is
`fc6c5747a4359ff94a58071054e124ba1449cc89`.

## Accepted behavior

A locally HMAC-signed current-owner Mini App fixture authenticated through real
Zitadel. A legal-name form write advanced the encrypted owner record from0 to1.
An exact retry returned the same receipt, including after stopping and starting
the same bot container. The shared protection key and session survived. No Loom
run or input was created by the form action.

Three actual messages were sent through the authorized Telegram Web private chat.
The first started one real Loom run. It received redacted form-action metadata,
read current presence/version through the broker and asked for the missing field.
The plain-text reply continued that run as steering input. A native `module_call`
wrote the synthetic passport value, advancing version1 to2. A second form update
advanced version2 to3 while the run waited. The final plain-text reply continued
the same run, read version3 and completed it. Its final answer was visibly delivered.
There were three completed inbound tasks and two accepted run inputs. No question
ID or hidden text tag was required to route these replies.

The first form receipt remained historical version1 after later changes. A stale
version0 write returned409 without mutation. The manual-action journal contains
the two form operations at versions1 and3; the three protected mutation receipts
include the agent write. Dedicated encrypted storage, receipts and journal contain
no plaintext synthetic field values. Neither form name was copied into provider
requests. The passport was explicitly sent in Telegram: that input necessarily
belongs to ordinary Telegram/Loom history and is not claimed to be absent there.

Ten protected business-table hashes remained unchanged, including profiles,
registrations, passes, pairs, payments, events, settings and request expectations.
Eight companion container fingerprints and the poller were preserved. The bot
restart exited0 without OOM and reused its original container. Logout returned204;
the same session subsequently received401 on details reads. Only the synthetic
protected details and their receipts/journal were retained. Preserve the encryption
key with these records; this acceptance does not reset or delete them.

## Observed QA failure and recovery

The first QA implementation attempted an external write inside `script`. Loom
exposes external reads there; the write returned `tool_failed`. Current status
remained version1, and no write operation was created. The QA helper was corrected
to use the documented native `module_call` write transport. A fresh independent
source review found no actionable findings. The failure and recovery are retained
in the checkpoint, and acceptance continued in the same run without another
Telegram message or replacement task. This was a harness error, not evidence of
a Loom defect.

## Evidence and limits

Ignored evidence: `.cache/shuttle-live-test/miniapp-details-native-qa/checkpoint.json`.
It records accepted status, original IDs, status readbacks, call-output hashes,
redacted journal receipts, the restart and the harness failure. Private session
credentials are in a separate ignored file and must not be published. The helper
is `.cache/shuttle-work/miniapp_details_native_qa.py`.

Real IAM, HTTP APIs, Telegram transport, Loom orchestration and broker calls were
used. Model responses were supplied manually through the owned mailbox provider.
This proves transport and state continuity, not unattended model interpretation.
The form API fixture used locally signed proof, not Telegram-issued initData or a
trusted Telegram WebView. Separate loopback browser/Node component acceptance is
described in [the implementation document](shuttle-miniapp-registration-details.md).
No browser sessionStorage inspection or saved screenshot artifact is claimed.

Event-specific prerequisite continuation, native registration/pass parity and
trusted WebView entry remain separate gates. Global legal-field completion does
not imply registration, pass allocation, payment or event consent. No production,
commit or push was performed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
