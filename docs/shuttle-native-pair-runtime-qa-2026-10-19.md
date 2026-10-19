# Native pair form and agent runtime QA — 2026-10-19

The bounded runtime gate passes on an isolated owned stand. Real Zitadel and
Loom serve actual Edge browser forms and reactive agent tasks; Telegram transport,
its browser SDK/proofs and model responses are synthetic. This is not acceptance
of unattended model interpretation or the real Telegram WebView. The primary
stand was still on its earlier image, schema59/mvp68 and133 Loom runs. The
subsequent [primary/Telegram entry gate](shuttle-native-pair-primary-qa-2026-10-19.md)
is recorded separately.

## Candidate and setup

The candidate is `sha256:7706bc58a464dd0dd0e3d8f85e36d32dd7666efec08644ef373ebf1e5100f6dc`.
It uses the installed anchor image as its local base and replaces only the bot
with the pinned Go1.27.2 Linux binary. The binary SHA256 is
`a8b3d0dd0f5c9d811edacefd653371a5f9e2cf8cbf38123aa809f0ccecf0e9bd`.
The avatar helper, licenses and CA bundle come unchanged from that base. This
fixture image is not a fresh build of the release Dockerfile. The normal source
[component gate](shuttle-native-pair-ui.md) retains its separate checks.

The stand used nine isolated services, a separate CA, databases, volumes, mailbox
and mock Telegram. It reused no primary credentials, network or users and made
no change to a Loom developer stand. Three synthetic users were provisioned
through the actual Mini App bootstrap and real IAM. Six event/leader-waitlist
fixtures were inserted explicitly into this disposable database. Registration
creation and its prerequisites were not under test in this fixture setup.

## Executed paths

1. Native invitation and native acceptance: the inviter selected the event and
   partner code in the actual form. The target first pressed Accept without
   checking the role; no write occurred. Explicit follower consent then created
   one waitlisted pair. Both browser sessions signed out.
2. Agent invitation and agent acceptance: two explicit synthetic user messages
   started separate real Loom runs. Reads used the offered `script` tool and
   writes used `module_call`. Each task completed with its owner token.
3. Native invitation followed by target agent acceptance.
4. Agent invitation followed by target native acceptance.
5. The same owner invited through the form, then asked Loom to revoke that exact
   outgoing invitation. The fresh outgoing read selected the original invitation;
   the reactive prompt also contained the owner's native receipt.
6. The same owner invited through Loom, then revoked the original invitation in
   the actual form. This created a second receipt rather than rewriting the
   original invitation receipt.

The first four paths form four waitlisted pairs. The final two release the
inviter's hold without registering the target. They leave the inviter's own
waitlist version at3 and no active pair membership for those events. Same-owner
continuation here covers invitation/revoke, not every registration/payment action.
Each native browser action issued exactly one pair POST, with no browser script
errors. The form displayed event-zone deadlines and historical receipt labels.
The native screenshot was also inspected visually. Real Telegram rendering and
mobile layout acceptance remain separate.

The deterministic provider inspected actual pending requests and answered those
same requests. An initial read attempted through `module_call` returned
`use_script_for_reads`; the original run then used `script` successfully. The
rejected probe had no business effect and remains in the evidence. It was a
fixture-contract correction, not a demonstrated Loom defect.

## Readback and restart

Final readback finds12 distinct pair operation keys, four waitlisted pairs,
eight active members and no passes. All six Loom runs and all bot tasks are
completed; no outbox item remains undelivered. Own native operation receipts
appear in later reactive prompts, and the tested other owner's receipt key is
absent. This does not make historical receipts new consent.

For both same-owner paths, the complete original invite result is compared with
the final archived operation result. Native operation IDs are bound to browser
requests; the agent invite is bound by its unique invitation ID and complete
original tool result. The native revoke result is also compared in full. The
historical invite remains pending in its immutable receipt, while the current
invitation is revoked. An earlier state-only check was insufficient to establish
full-result preservation; the archive comparison closes that review finding.

After the initial four paths, the same isolated bot stopped through SIGTERM in
0.610 seconds, exit0, without OOM. After restart, hashes of eight canonical event,
registration and pair tables matched and Loom remained at four runs. The two
same-owner cases were executed afterwards; the four-run restart result is not
presented as a later six-run restart test.

## Cleanup and remaining work

Validated custom dumps of the bot, Loom and Zitadel databases and service logs
were retained. All nine owned services, their network and volumes were removed.
PostgreSQL had a30-second cleanup timeout; the bot retained its5-second hard
limit. No service reported OOM. One subsequent128MiB networkless pg_restore
container extracted pair receipt text from the dump without restoring a database;
it was removed too. No temporary acceptance container remains. The candidate
image and ignored evidence remain for the next stage.

Independent runtime review found no issue in the four-path scope. Same-owner
review found the state-only receipt comparison gap; the full-result archive
comparison supplies the missing proof. Source review does not replace the actual
execution evidence. The component's code and documentation reviews are recorded
separately. Evidence lives under `.cache/shuttle-work/native-pairs-runtime-v1-qa/`.
No commit, push or production deployment was performed.

Next at this historical gate: perform the controlled primary candidate/rollback/
install gate and then
real Telegram `/pairs` entry/form acceptance, keeping actual business writes
within the authorized account/chat. Full native registration/pass parity remains
open. Interactive CA/zitadeltg linking still waits for the deferred user session.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
