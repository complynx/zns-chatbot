# Native pair pass assignment

Status: implemented and installed on the owned local primary at schema64 /
`zinusya/mvp-77`; see [mvp77 primary installation](shuttle-pair-terms-primary-qa-2026-10-20.md).
The [schema63/mvp72 report](shuttle-native-pair-assignment-primary-qa-2026-10-19.md) records the earlier installation. This extends the
[canonical pair foundation](shuttle-native-pair-assignment-foundation.md).
Source/component checks and the owned browser/real-IAM/Loom four-path gate pass.
Local-primary installation and binary rollback on the expanded schema pass.
The broader [registration/pass matrix](shuttle-registration-pass-parity-plan.md)
remains open.

## Permission and shared operation

An active Mini App owner needs current global `zinusya.admin` permission or
administrator authority for the specific event. Solo assignment retains its
global-only policy. Session, actor and permissions are derived server-side.
Native APIs, agent APIs and both continuation directions use the same six
business arguments, canonical proposal, immutable quotes and one two-pass receipt.
Preparation and adoption reserve no capacity and supply no human consent.
Confirmation requires the exact proposal and arguments plus explicit approval.

Event, actor and session barriers precede the proposal row lock. The actor lock
stays SHARE; there is no lock upgrade for native access. Session validity and the
local IAM invalidation/expiry fence are rechecked after waits and before commit,
including when replaying an existing receipt. Network IAM calls stay outside
these transactions. Existing membership, capacity, versions, legal requirements,
module availability, quote and closing-time checks govern every new effect.

Completed receipts remain readable after proposal expiry, sales closing or
module disablement under current authority. They describe the original result;
current pair and pass state must be read separately. An active account can inspect
only its own proposals, even when it can administer another actor's event.

## Form and recovery

Choose an event and a pair explicitly. Both participant display names and
Telegram IDs are visible, followed by separate role prices, tiers, deposits and
deadlines. Stored proposal timezone governs display; older proposals without it
use explicitly labelled UTC. Canonical descriptions remain verbatim.

Pending/recent lists and exact recovery responses include role-bound participant
identities from the original pair memberships. This works after the pair leaves
the candidate queue and after a fresh browser launch. Display names are current
profile metadata, separate from the frozen terms. Legal and passport fields are
not included. Revoked participant profiles are not read; their Telegram ID remains
an account locator for historical inspection.

The browser persists owner and operation/proposal/concurrency identifiers before
POST. It does not store names, prices, credentials or consent. Lost replies use
read-only lookup of the same operation. An unresolved confirmation retains its
operation and blocks a replacement. Retrying confirmation needs fresh explicit
consent. Closing a preparation is local only and does not cancel the server
proposal. Owner changes fence late responses and clear the presentation.

Corrupt, oversized or unreadable local recovery identifiers block mutation but
keep authorized server history and receipt inspection available. Storage failure
never authorizes a new preparation, adoption or confirmation. Read-only inspection
preserves the existing journal, including an unknown confirmation, and does not
enable consent until valid identifiers can be persisted.

## HTTP and history

All routes use the existing Mini App bearer, origin checks, strict bounded JSON
and no-store responses under `/app/pair-assignments/`:

- `GET events?after=UUID` and `GET events/{event}/candidates?after=UUID`.
- `GET proposals?after=UUID`, `GET proposals/{proposal}` and
  `GET operations/{operation}`. Operation keys are opaque single encoded segments.
- `GET recent`: up to twenty canonical groups from the owner's native history.
- `POST prepare`: `operation_id`, six-field `arguments`, optional
  `resume_proposal_id`. Confirmation fields are forbidden even if null/false.
- `POST confirm`: `operation_id`, exact `arguments`, nonzero `proposal_id`,
  `confirmed: true`. A resume field is forbidden even if null.

Paged reads contain at most twenty rows. Current event access is filtered before
the limit. Cross-event lists acquire no event locks after the actor/session
barrier; exact reads use one pool lease at a time and retain current authority.
Authentication failure returns 401, permission loss 403, unavailable actor-owned
objects 404, changed terms/capacity/eligibility 409, and invalid input 400.

Migration 63 adds immutable native prepare/confirm locators in the same transaction
as proposal adoption or the original receipt. Retries preserve their first
timestamp and original snapshot versions. Populated history blocks Down until
explicit export/reconciliation; an empty Down preserves canonical proposals.

The next user-triggered agent request receives at most five currently authorized
history locators recorded strictly before its task creation. These are historical
untrusted metadata, not live state, consent or a proactive instruction. A later
alias or result cannot enter an earlier task's context. The agent can read the
original proposal/result and adopt an unexpired proposal under a new operation
key, then ask for fresh exact approval. Native writes do not create agent tasks.

## Acceptance boundary

The initial `mvp-71` full local Go run passed 1037 root scenarios with no failures and six
platform/helper skips. These checks use Go 1.27.2 on Windows with CGO disabled;
they do not establish race or Linux runtime acceptance. All 285 Node UI behavior
cases pass, including sixteen pair-assignment cases. Final DB/API/task unit
checks and lint pass. Isolated sqlc compile/generate reproduces all three Go
files byte for byte. The final Linux/amd64 binary cross-build also passes.

A subsequent bounded `mvp-72` follow-up makes the same participant display
identities available to agent candidates and actor-owned exact/pending proposal
reads. An event-only administrator no longer needs an unrelated global user
lookup to identify the pair. Current event authority still scopes every read;
revoked candidates are filtered before pagination. The shared helper reads no
revoked profile name and changes no frozen description, command hash or quote.
Tool guidance asks for both recognizable names and Telegram IDs before approval.
Fifty-eight affected PostgreSQL root scenarios, affected unit checks, lint and
two fresh independent reviews pass. Isolated sqlc reproduction and the new Linux
cross-build pass. The earlier full run is retained as the `mvp-71` baseline; it
was not repeated for this bounded follow-up. Evidence is under
`.cache/shuttle-work/pair-agent-identities-v1/`. Both temporary PostgreSQL fixtures
were archived, fully decoded, stopped cleanly and removed with their volumes.

Three review rounds end with clean independent correctness and quality reviews.
Two substantive findings were fixed: unreadable recovered recipients and blocked
read-only server recovery after a local journal error. The first scoped database
run retained an expiry-observation timeout whose exact cause was not proven;
the strengthened observation uses the held connection and checks the specific
denial. The next run retained a duplicate profile fixture failure, corrected
before the passing full run. Initial lint findings, a cross-realm JavaScript
fixture assertion and a Windows sqlc verification-path failure are also retained.

Ignored local evidence is under
`.cache/shuttle-work/native-pair-assignment-session-v1/`: reviewed `scope-v3.json`,
`review-loop-final.json`, `pg/pg-full-v1.json`, final lint/unit/UI logs,
`sqlc-verify/result.json`, `linux-build-v2.json` and `primary-preserved.json`.
At that source-stage checkpoint, the owned primary remained eleven services,
schema61/mvp69 and 134 Loom runs.

## First runtime attempt (not accepted)

The isolated `pair-assignment-r1` stand used the mvp72 image
`sha256:e2a078b2b8ef897b80d943bb2b31d71d92b528028acb37cf7b0a22712308cc43`,
real Zitadel and Loom, mock Telegram and actual headless Edge.
Ten synthetic identities and four waitlisted pairs were created through APIs.

With Loom stopped, the form assigned two distinct passes atomically. The
confirmation committed with HTTP 200 and its response was deliberately lost.
The browser controller then reused one-time Telegram initData for a second
launch; the application rejected that replay. The failed attempt remains in
local evidence. A separate fresh authenticated launch recovered the exact
original proposal and complete receipt through reads only, with zero Loom runs.
No prepare or confirm was repeated.

The event-only administrator agent path read both names and Telegram IDs through
real Loom tools and reached a pending approval, without assigning passes.
Acceptance stopped because the delivered approval showed frozen account UUIDs
but omitted recognizable participant identities. Supplying names in Loom's
`confirmation_text` does not display them in agent reply mode. At that point, the whole four-path runtime gate was not accepted. The source
fix and subsequent r2 acceptance are recorded below.

Ignored evidence is under
`.cache/shuttle-work/native-pair-assignment-runtime-v1-qa/`: `runtime-findings.json`,
original and recovery browser results, the approval transcript and
`cleanup-proof.json`. All three database archives were fully decoded before the
nine owned services, volumes and networks were removed. The primary remained
unchanged. This failed attempt is not an installation or production acceptance.

## Approval presentation follow-up

The task delivery path now resolves the actor-owned canonical proposal through
its original operation or adoption alias. It checks current event authority,
original arguments and expiry before displaying both current participant names,
Telegram/account IDs and the unchanged original description. Names are literal
text, separate from frozen business terms. Configured deposit amount/share,
frozen final top-up deadline, IANA timezone and approval expiry remain visible.
Model-authored captions cannot replace these service-verified details.

If the form has already completed this same operation, the prompt explicitly
asks to reconcile its historical result. It does not suggest assigning again.
Other approval methods retain their existing routing.

Four focused delivery behavior roots and ten related PostgreSQL scenarios
passed (14 total, no failures or skips). Affected unit checks, final lint and
Linux amd64 cross-build passed. Two independent final reviewers found no
issues. The original lint/formatting failures remain in ignored evidence under
`.cache/shuttle-work/pair-approval-display-v1/`, alongside `scope-v3.json`,
`component-acceptance.json` and the PostgreSQL results. Its temporary database
was archived, fully decoded and removed. This did not rerun the earlier full
1037-test baseline or establish Linux/race acceptance.

## Corrected owned runtime gate

The fresh isolated `pair-assignment-r2` stand passed all four paths on image
`sha256:6e1ca865cb8095c8456822c58763b91d32d5c388f0792b99322ce3ec88caefd8`:

- Form only, with Loom stopped: one confirmation committed, its response was
  lost, and a fresh browser context recovered the exact receipt through reads.
  Each simulated Telegram launch used new one-time initData; zero Loom runs.
- Agent only: an administrator of just that event read and displayed both actual
  names and Telegram IDs, obtained explicit approval and completed the operation.
- Form to agent: the agent adopted the original proposal and terms. A normal
  bot restart while awaiting consent preserved the same task/run, approval,
  proposal, alias and native history. Explicit approval completed that operation.
- Agent to form to agent: the form confirmed the original agent proposal, then
  Loom reconciled that complete receipt and succeeded without another assignment.

The result is eight assigned registrations, four assigned pairs, eight distinct
passes, four canonical proposals and four assignment receipts, one adoption
alias and four native action records. All three Loom runs and Shuttle tasks
completed, and all outbox records were delivered. Resolution output was checked
against the complete original receipt and a succeeded operation on the same run.
Both mixed-path recent-history inspections made no mutation calls.

The measured bot stop took 0.703 seconds, exited zero and did not use OOM/kill
recovery. The original pending approval and durable business snapshots survived.
The browser screenshots were visually inspected for participant names, local
Minsk deadlines and historical receipt state.

A QA counter initially included the fixture's separate invitation-acceptance
receipt. The failed observation is retained in `checkpoint.json`. The counter
was corrected to `kind='assign'`, reviewed, and the existing pending approval
was revalidated by reads before its first confirmation; no begin request was
replayed. This was a controller error, not an extra pass assignment.

Ignored r2 evidence is under
`.cache/shuttle-work/native-pair-assignment-runtime-v2-qa/`:
`runtime-acceptance.json`, `controllers-final.json`, browser results/screenshots,
`checkpoint.json` and `cleanup-proof.json`. Three complete database archives
were decoded before the nine owned services, volumes and networks were removed.
The eleven-service primary stayed schema61/mvp69 with its 134 Loom runs.

This gate uses a controlled synthetic model provider with actual Loom, Zitadel,
mock Telegram and headless Edge. It does not establish autonomous model
comprehension, real Telegram WebView acceptance, Linux/race coverage, the whole
registration/payment matrix or production readiness. The subsequent local
primary installation and binary rollback are accepted in the linked report.
Next are the remaining participant payment, top-up and ambassador-confirmation
paths and the complete registration-through-assignment journey.


## Deposit presentation follow-up

The [source mvp76 follow-up](shuttle-pair-deposit-presentation-2026-10-20.md)
shows the original minimum amount/share in both pending and historical views.
The [mvp77 structured terms follow-up](shuttle-pair-assignment-terms-2026-10-20.md)
now replaces technical description text for new proposals. Old proposals retain
an original-description fallback. Both directions now pass the separate
[owned binary compatibility gate](shuttle-pair-terms-binary-runtime-qa-2026-10-20.md).
The primary is now mvp77; installation and rollback pass in the separate primary report.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
