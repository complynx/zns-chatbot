# Native pass payment runtime QA, 2026-10-19

## Accepted scope

Source schema64 / `zinusya/mvp-73` passes the owned isolated runtime gate with
real Zitadel, the pinned real Loom service, mock Telegram and actual headless
Edge forms. The provider is controlled by the QA player. This proves transport,
state, tools and consent execution; it does not prove autonomous language
understanding or acceptance inside a real Telegram WebView.

At this isolated gate the owned primary was schema63 / `mvp-72`, with eleven
services and134 Loom runs preserved. Subsequent [schema64/mvp74 installation](shuttle-native-pass-payment-primary-qa-2026-10-20.md)
and the authentication fix are accepted separately. Production was not changed.
Whole registration/pass and pair-joining parity remain open.

Candidate image:
`sha256:64f43c6e94c1727237c678a5a92e41eb6b5d94de87fe6fc0825c2ff27136cb25`.
Its Linux amd64 binary is the unchanged reviewed component-gate binary:
`08eff4a9b2546c5a7b4d23976bf38ca09ebda646d9479e4c47562e1ade09a8f3`.

## Four participant paths

Each synthetic participant has one API-assigned pass priced at 100.00 BYN.
The required deposit is 30.00 BYN, followed by a 70.00 BYN top-up. No real money
or real participant data was used.

- Form deposit and form top-up; the ambassador uses forms for both reviews.
  Loom is stopped throughout both financial stages, with zero Loom runs during
  that interval. The accepted attachment tasks are handled separately afterward.
- Agent deposit and agent top-up; the ambassador uses the agent for both reviews.
- Form deposit followed by agent top-up for the same participant; the ambassador
  uses the agent. The later actual model input contains the native deposit
  locator, and tools refresh current evidence and balance before the new write.
- Agent deposit followed by form top-up for the same participant; the ambassador
  uses forms. The form shows the confirmed 30.00 BYN and remaining 70.00 BYN.

Final PostgreSQL read-back verifies eight files, eight evidence rows, eight
submission operations and eight confirmed ambassador reviews. Each pass is
credited exactly 10000 minor units; the total is 40000 synthetic BYN minor units.
There are four native submissions, four agent submissions, four native reviews,
four agent reviews and four native history markers. All eight complete original
submission receipts still equal their initial pending snapshots after review.
No task, outbox delivery or Loom run remains unfinished.

## Recovery and file proof

The initial form submission commits once, then its successful HTTP response is
lost deliberately. A fresh browser context and new Mini App session recover the
original operation by GET. The browser records one submission POST.

A graceful bot stop takes 0.828 seconds, exits with code0 and no OOM, and preserves
the exact original approval. That approval later expires while QA observers are
being corrected. Loom returns `approval_expired`; it does not credit money. An
explicit refusal retires the old action. A separate user request creates a new
workspace, revision, proposal and approval before the valid confirmation.
The expired workspace has no financial review operation. This is not evidence
of a timely confirmation of the original approval after restart.

Eight browser results verify current served assets, complete context shutdown,
and nonempty protected-file bytes. The actual UI blob URL and the protected fetch
clone have matching byte counts, SHA-256 and MIME. This is byte-delivery evidence;
the tiny synthetic PDF fixtures do not establish PDF rendering quality. Selected
focused screenshots were also inspected: deposit terms, pending receipt,
ambassador preview, confirmed deposit at form handoff and final zero balance.

## Retained QA failures and test policy

The first pilot stopped before ambassador writes because a CDP body observation
reported zero bytes. Direct authorized reads returned the original bytes. The
replacement observer measures both the browser fetch clone and actual UI blob.
The original failure and pilot archives are retained; it was not rewritten as PASS.

The second stand exposed observer assumptions about Markdown fences and terminal
newlines: Telegram receives text plus entities. The corrected observer compares
the exact literal terms using UTF-16 entity offsets. A plain assistant reply also
cannot finish an unresolved blocked action; QA now uses explicit refusal before
a fresh request. These are QA corrections; the product binary did not change.

Two manually controlled provider requests exhausted Loom's documented whole-run
`sponsored_attempt_limit: 2` while waiting. An ordinary resume retains that cap.
For this temporary stand only, the cap was raised to8 and the same pinned Loom
container was restarted gracefully. Explicit API resume continued the original
runs; no media was resent and no financial tool ran during acknowledgement.
This does not establish automatic recovery from provider-budget exhaustion.
The initial operator credential omitted resource-owner scopes and received401;
recovery used the exact recorded rejection and key with the bot's normal scopes.
No new Loom defect was established by these observations.

## Evidence and remaining gates

The local ignored evidence is under
`.cache/shuttle-work/native-pass-payment-runtime-v2-qa/`: `runtime-acceptance.json`,
`checkpoint.json`, eight browser result files, screenshots, policy/recovery
journals and `cleanup-proof.json`. Earlier pilot evidence remains under
`native-pass-payment-runtime-v1-qa/`.

Three custom PostgreSQL archives (Shuttle, Loom and Zitadel) were fully decoded
with `pg_restore --file=/dev/null` before cleanup. Restore into fresh databases
was not tested. Both temporary stands, their volumes and networks were removed;
the second cleanup removed nine services and verified primary preservation.

Source checks remain 64 PostgreSQL roots, 304 UI behavior cases, affected Go
units, lint0, exact sqlc regeneration and Linux cross-build. The final source
review, observer/recovery reviews and final acceptance-gate review have no
unresolved actionable findings. The index remains empty; this slice is unpushed.
The owned runtime evidence does not close a new full-repository or race gate,
real Telegram WebView acceptance or deployment. Subsequent primary installation
and rollback are recorded in the linked installation report.

The next whole registration/pass journey gate remains open after the subsequent
primary installation. Continue orders, meals, massage and linking
against the [parity plan](shuttle-registration-pass-parity-plan.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
