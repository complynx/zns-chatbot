# Combined IAM/Loom pre-run outage acceptance — 2026-10-17

Accepted scope: one actual Telegram request admitted while both own Zitadel and
Loom were stopped, retained across a cold restart of the same bot container, then
completed through one new Loom run and one final Telegram delivery after both
dependencies returned. This is pre-run recovery, not recovery of an already
created run, cache availability, expired-token recovery or production readiness.
No Go runtime code or service configuration changed for this gate.

## Real execution

Only the disposable `shuttle-live-test` stand and the authorized current account
in `@complynx_test_bot` were used. The image stayed `904344b4`, schema46 /
manifest `mvp-49`. No Loom developer environment or production was changed.

Exact IDs, images, security/mount/environment settings and initial fingerprints
were frozen for all ten Compose containers. Both private database dumps were
verified with pg_restore --list before the driver marked preparation complete.
The seven non-target service fingerprints and the required counted provider
were checked unchanged. No extra container or volume was created.

1. Own Loom and Zitadel were stopped using their exact recorded IDs. The bot,
   PostgreSQL, Telegram-poller and gateway stayed running. PostgreSQL itself
   was not restarted.
2. The current account sent exactly once: «Покажи мои активные записи на массаж.
   Ничего не меняй.» The task was durably admitted while both dependencies were
   confirmed stopped. It reached awaiting_authorization with no RunID or Loom
   auth session. Task/run totals changed from178/118 to179/118.
3. The same bot container stopped cleanly with exit0 in0.781seconds and started
   while IAM and Loom were still stopped. Readiness returned. The same task,
   conversation and request hash remained; state was still awaiting_authorization
   and no Loom run or final failure had been created.
4. The same Zitadel container was started and checked through the existing trusted
   TLS gateway. An initial HTTP502 during startup was retried until discovery
   returned the expected issuer. The same Loom container then started and became
   ready. No container was recreated and no credential/configuration was edited.
5. The original task acquired one auth session and created one Loom run. A bounded
   manual provider supplied a history summary, one read-only
   `modules.zinusya_massage.list` call for the synthetic event, and the final answer.
   The real module call used the current user authorization. It returned one
   active booking; no mutation/confirmation tools were called.
6. The actual Telegram chat displayed the Russian answer with 25 October2026,
   03:00–03:15, Europe/Berlin GMT+01:00 and Тестовый Алекс. The final task and run
   were completed, and one outbox row was delivered. Final totals were179/119.

Task: `e25e6c77-2734-4621-ae2a-af5e59831126`.
Run: `3f9ef373-30e6-4cdf-ba7d-932987143ba6`.
Auth session: `da363e57-af7c-440f-8afb-94d485dbdbff`.
Final outbox: `8e910b7f-15b5-5c95-8d97-32fe2093b931`, Telegram receipt4548.
Authenticated subject: `2eae5fba-9685-41e6-a64e-3a28c417fb4b`;
the final Loom owner subject matched it. The auth session and RunID remained the
same from the observed accepted run through completion.

## Preservation and limits

All53 selected domain-table hashes were unchanged, covering event, registration,
assignment, pass, payment, pair, order, avatar and massage prefixes present in this
schema. The proof records the exact table list. All previous task rows and all
outbox rows outside this one task were also unchanged. Final idleness and
readiness passed; all three target containers ran with original settings, no OOM
and unchanged automatic restart counts. Normal lifecycle start times changed.

The provider was manually operated; token counts were synthetic estimates.
This proves actual IAM/Loom/Telegram transport and worker recovery, not unattended
model behavior, billing accuracy or independent model interpretation. The summary
was bounded and the read result was checked before composing the final answer.
No raw credentials, tokens or private profile values appear in this report.

## Review and evidence

Independent correctness and skeptical quality reviews both required two fixes:
mark prepared only after both verified backups, and attempt to start every exact
target container before waiting for any one service during incomplete-attempt
restoration. Both were fixed before service mutation. Five fake-peer critical
backup/restore checks passed; a fresh final review found no actionable findings.
Actual read-only task/outbox SQL paths were checked before the outage.

Ignored evidence is under
`.cache/shuttle-live-test/combined-service-outage-prerun-v1/checkpoint.json`, with
private database dumps alongside it. The driver is
`.cache/shuttle-work/combined_service_outage_prerun_qa.py`; focused fake-peer
results are under `.cache/shuttle-work/combined-outage-driver-unit-v2/`.
No actual outage attempt failed in this gate. Earlier failures in other gates,
including the [first SIGKILL arm attempt](shuttle-massage-hardkill-real-qa-2026-10-17.md),
remain retained. No staging, commit, push, PR, issue or production action occurred.

## Remaining acceptance

Repeat combined dependency downtime after a Loom run already exists, including
its shutdown pause and same-run recovery across a bot restart. Verify recent
cached credentials versus expired/cold credentials as separate cases. Preserve
D16: module_discovery_unavailable may encode denial as well as transient failure,
so this untyped pause must not be blindly resumed. Specialist/admin massage
lifecycle, notifications, remaining Python parity and unattended model behavior
also remain open. The full project goal is not complete.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
