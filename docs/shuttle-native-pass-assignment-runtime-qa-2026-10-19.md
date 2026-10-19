# Native solo assignment: owned-runtime acceptance, 2026-10-19

The four interface paths passed on the isolated `pass-assignment-r1` stand.
The stand used the real bot, PostgreSQL, Zitadel and Loom, a local Telegram
simulator, and actual headless Edge forms. Loom's provider was controlled and
synthetic. This does not establish unattended model behavior, a real Telegram
WebView write, complete pass/payment parity, or production readiness.

Candidate image: `sha256:17763b12faaa79402960c3c37f8b76fa1662d20a7b19e905fde083a183bb5770`.
The Linux binary SHA-256 is
`96183fd0f0ccc0bed16f1eb6a6e153285c246c94319418ce51088a56dedb1f26`.
The candidate uses schema 61 and manifest `zinusya` / `mvp-69`.
It is not installed on the primary by this gate.

## Accepted paths

| Path | Observed result |
| --- | --- |
| Native | Loom stopped throughout assignment. Actual form selection, proposal and explicit confirmation produced one pass. The accepted confirmation response was deliberately lost; a fresh browser context recovered the original historical receipt using Recent form operations and the same operation lookup, with no second confirmation POST. |
| Agent | A real Loom run prepared and executed the assignment after exact approval under the fixture administrator's real IAM identity. One pass and one canonical receipt resulted. |
| Native to agent | The form's original proposal survived an owned bot restart. Loom adopted it under a new envelope operation, retained its original terms and expiry, collected fresh exact approval, and returned the same canonical business result. One alias and one pass resulted. |
| Agent to native | The form discovered the actor-owned proposal from a real Loom run, confirmed its original operation with explicit consent, and Loom reconciliation reused the original result. No second pass resulted. |

Fixtures contained a fresh administrator with grants in both API and Loom
projects, four participants, and four events. Participant waiting-list joins
used authenticated domain API calls; no pass or payment was seeded with SQL.
Event setup and fixture identity configuration are not user-flow acceptance.

Final evidence shows four assigned registrations, four passes, four canonical
proposals and matching canonical receipts, one transport alias, and three
completed real Loom runs. No unfinished tasks or outbox deliveries remained.
No payment was recorded by assignment.

The bot graceful restart took **0.782 seconds**, exited 0, and had no OOM.
Original proposals, receipts, tasks and run identities survived. This proves
that restart case; it does not establish a forced-kill or simultaneous
IAM/Loom outage gate.

## Preserved failed observations

The initial native browser observer timed out waiting for an unknown-result
banner after the confirmation had committed and its response was deliberately
lost. Its failed result is retained. No DOM snapshot established the banner's
state then, so its cause remains unproven and banner presentation is not accepted
by this runtime gate. The original assignment was not replayed; subsequent
fresh-context reads recovered its committed receipt.

Two follow-up browser attempts reused consumed synthetic Telegram launch data.
The form correctly required a new launch. Freshly signed launch data resolved
those fixture failures; both original failed results remain retained.

The first native-to-agent observer expected `resume_proposal_id` in canonical
approval arguments. The actual contract correctly excludes that transport field.
The observer was corrected and the same task/run continued, without replaying
the module call. Its failure remains retained.

## Cleanup and primary custody

Three custom-format database dumps were validated and retained, with logs and
failure receipts. All nine owned services, their volumes and network were
removed. No QA container or active QA process remained.

The bot, PostgreSQL, Loom, IAM and other fixture services exited 0 without OOM.
The mock Telegram server required termination after its five-second stop limit:
exit 137, without OOM. This fixture cleanup failure remains recorded; do not
report that every service stopped gracefully.

The primary remained on its original image/schema with 11 services and its
protected domain preserved. No primary business write, new primary Loom run,
real Telegram message, production change, or Loom developer-stand change was
part of this gate.

Authoritative local evidence is under the ignored
`.cache/shuttle-work/native-assignment-runtime-v1-qa/` directory:
`runtime-acceptance.json`, `checkpoint.json`, `cleanup-proof.json`, individual
browser/provider results, and validated archives. Local credentials are excluded
from Git.

The next gate is controlled local-primary schema expansion and
candidate/old/candidate binary rollback, followed by the remaining
[native/agent registration and pass parity work](shuttle-registration-pass-parity-plan.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
