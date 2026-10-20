# Knowledge notice runtime acceptance, 2026-10-20

Source: `cff00829`, manifest `mvp-82`, additive migration66. Candidate image:
`sha256:984136110934f0ab747b58a08a797da47fc64bedae42c013df3e7bf98e405fad`.
The owned primary remains schema65/mvp81. No production installation is claimed.

## Stand and scope

The isolated `zinusya-mock-kb-notices-r1` project uses nine bounded services,
real PostgreSQL, real Zitadel and real Loom. Telegram delivery and five
synthetic accounts are local fixtures. This gate sends no real Telegram
messages and changes no Loom developer stand. Users and rights were created
through authenticated bootstrap and IAM/API flows, not business SQL seeding.

The controller records mutating intents before dispatch and refuses uncertain
replay. Independent review corrected API projection assertions, persisted event
request locators and restart timing checks before execution. A fresh final
review found no actionable findings. No product defect was found by this gate.

## Observed behavior

Six delivery scenarios pass:

- A native proposal produces one reviewer notice. Its proposer and moderator
  can read it; an unrelated user cannot. Native rejection produces a Russian
  decision notice. Confirming the same operation returns the same receipt.
- After an explicit HTTP429 rejection, the notice remains retryable while the
  bot is stopped. Starting the same bot delivers the original notice once.
- Revoking named moderator rights before a retry blocks delivery. Restoring
  the fixture does not create a replacement recipient/kind record.
- An accepted send followed by a lost response remains `delivery_unknown`
  after restart. No automatic duplicate appears during ten seconds of
  observation, covering the one-second maintenance cadence.
- Active-event administrators receive pending proposals. After the event ends,
  a named history moderator receives the pending work; the former event
  administrator loses access. Historical rejection notifies the proposer.
- SIGTERM during an accepted, held send persists uncertainty before restart.
  Releasing the original mock response and restarting does not send again
  during the same ten-second observation window.

The three graceful stop calls took0.641,0.625 and0.703 seconds, with exit code0.
The same bot container restarted; companion container fingerprints were preserved.
No OOM was observed. This is SIGTERM evidence, not SIGKILL acceptance or a
long-duration no-duplicate guarantee.

API checks cover proposer, named moderator, unrelated user and global
administrator. Non-administrators see only their own recipient records. The
response field allowlist excludes proposed text, outbound payload, attempt ID
and Telegram receipt. Global administrators can inspect deployment metadata.

Final knowledge state is six suggestions, eleven operation receipts and nine
notices: six delivered, one blocked and two unknown. No article or revision was
published. Native actions launched no Loom run.

## Reactive Loom readback

One subsequent synthetic user message creates one actual Loom run and one
completed bot task. Through the offered CodeAPI tool, Loom reads the original
submission receipt, the current rejected suggestion and the user's delivered
decision notice. The receipt still describes the original pending submission;
it does not override the newer review state. The seven knowledge tables remain
unchanged during that run. One final reactive reply is delivered.

The provider responses are controlled synthetic fixtures. This proves actual
orchestration, IAM and tool calls, not model comprehension, real tokenization,
cost or autonomous moderation quality. Native-to-agent write handoffs and
separate-device interaction remain separate acceptance work.

## Evidence and remaining work

Ignored private evidence is retained under
`.cache/shuttle-work/knowledge-notices-runtime-v1/`: `checkpoint.json`,
`readback.json`, provider request/response locators and runtime custody checks.
Packaging evidence is in `knowledge-notices-image-v1/`. Credentials and private
transcripts are not committed.

All five native sessions were logged out. Writer services were stopped, and
private dumps of `shuttle`, `loom` and `zitadel` were saved and fully decoded
with `pg_restore`. This verifies archive readability, not a restore. The nine
QA containers, project network and volumes were then removed. The cleanup proof
confirms the primary stand remained unchanged. No extra QA containers remain.

Exact authorized retry/dismiss tools for knowledge notices remain unimplemented.
An unknown notice must not be retried automatically or by repeating the business
action. Main installation, preceding-binary compatibility on schema66, remote
CI and model-driven moderation writes remain open. Browser CA/ZitadelTG joint
acceptance remains deferred until the user is available.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
