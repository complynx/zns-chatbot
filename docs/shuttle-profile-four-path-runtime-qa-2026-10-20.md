# Profile display name four-path runtime QA — 2026-10-20

All four display-name paths pass on an isolated schema64 / `zinusya/mvp-74`
stand. This is the independent profile name, not legal registration identity.
The existing primary image was used without changing product code or schema.

## Actual interface paths

Four fresh synthetic participants were created through signed mock Telegram
initData and real local Zitadel. No profile, legal details, registration, pass
or payment was inserted through fixture SQL or direct business API setup.

- **Native only:** with Loom stopped, `/profile` reads the empty name, `/name`
  creates an expectation and `/name Synthetic Native Command` saves it. The actual
  Edge profile form then changes the same name to `Synthetic Native Form`.
  Reload reads it back and `/profile` delivers that new name. No Loom run exists
  during this path.
- **Agent only:** real Loom reads the profile, calls `request_name`, and asks a
  real free-text question. The neutral name reply continues the same run. Loom
  reads the current expectation and calls `complete_name` with its exact ID and
  version, then delivers the saved name.
- **Native to agent:** `/name` creates the native expectation. The next explicit
  user request reaches Loom with that same expectation ID in context. The agent
  reads it and calls `complete_name`; it creates no replacement expectation.
- **Agent to native:** Loom creates an expectation and asks a question. The real
  profile form saves the name and clears that expectation while the run waits.
  The bot stops gracefully in 0.875 seconds, exit0, no OOM, then starts in the same
  container. The stored profile and original pending question are preserved.
  A later user message continues the same run, receives the form action locator
  and current name in context, and uses only `get` before replying. No second
  name write occurs.

Both actual Edge forms make one name POST, reload the saved value, sign out and
close their contexts. Both saved screenshots were visually inspected. The SDK and
initData source are mock Telegram with a context-local TLS exception. This is not
real Telegram WebView acceptance. A controlled provider chooses model responses;
real Loom executes the tools through authenticated bot APIs. This proves contracts
and orchestration, not unattended model comprehension.

## Persistence, delivery and retained failures

Final database reads assert four exact saved names, cleared expectations,
unchanged automatic language, and profile versions3/2/2/2. There are nine profile
operation receipts, two native form action markers, ten completed bot tasks and
three completed Loom runs. The outbox is drained. Legal details, registrations,
passes, payment evidence and payment reviews remain empty.

Three failed QA observations are retained. The first expected a transport envelope
that Loom had already unwrapped; the actual `request_name` had succeeded. Its exact
original output and stored expectation were reconciled, then the same run continued
without repeating the mutation. The second final-delivery observation failed;
subsequent readback already contained the exact final message. That retained
readback does not establish the failure timing. The corrected observer waits for
the actual message and reconciles its delivered outbox row. The third incorrectly equated the original question task with its later
steering task. Readback proves two completed tasks joined by `target_task_id`,
with the same original run. One exact final message and one delivered outbox row
are retained. No user message, final reply or business mutation was replayed to
make these observations pass. No product or Loom defect was established here.

Fresh normal and adversarial reviews covered the final three QA drivers. Both
final reviews have no actionable findings. Earlier review findings strengthened
new-task correlation, persisted-result checks, restart journals and cleanup.
The product source hash still matches the installed image build; Git index is empty.

## Cleanup and remaining acceptance

All eight non-PostgreSQL services stopped before final database archives were made.
All three custom archives were fully decoded with `pg_restore --file=/dev/null`;
this is not a fresh restore test. PostgreSQL then stopped cleanly. All nine exact
owned services, their volumes and network were removed, with no OOM. Primary
schema64/mvp74, its eleven services and134 original Loom runs remain preserved.

Local evidence is under `.cache/shuttle-work/profile-four-path-runtime-v1-qa/`:
`runtime-acceptance.json`, `checkpoint.json`, `cleanup-proof.json`, browser results,
screenshots, the three `observer-v*-failure.json` records and private logs/dumps.
Final review diff is `zinusya-profile-four-path-qa-v6.diff` in the local temp folder.

This accepts the four display-name happy paths and one original-question graceful
restart. It does not add live expiry/cancellation, two-tab conflicts, real Telegram
agent mutation, new full-repository/race acceptance or production readiness. The subsequent
[native joined registration/pair/payment gate](shuttle-joined-native-runtime-qa-2026-10-20.md)
now passes. Its agent-only and mixed counterparts remain open in the
[workflow plan](shuttle-registration-pass-parity-plan.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
