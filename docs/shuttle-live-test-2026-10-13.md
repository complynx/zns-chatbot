# Real-service Shuttle test stand acceptance — 2026-10-13

The isolated `shuttle-live-test` Docker project is running. Actual Zitadel 4.16.3,
Loom, Shuttle, separate PostgreSQL databases, sandbox and Docker discovery are
used. Source HEADs at verification: Shuttle `0c95bd1451e672ca2f1b67c098b2b6623a432f40`
and Loom `f0090ca3a600851a2d2bba186e58112318a48baa`; builds include working-tree source.
Original acceptance runtime images: Shuttle
`sha256:a9225b1542837099813fef94545ad388e483552820125b0c2caa822c8496caf7`,
Loom `sha256:03c57557ce84e8722f10f7d1de52085e8e5e971d308c2fafedc6610bdc7e38d9`.
The Telegram token is supplied only through ignored
`config/shuttle.test.env`; no token or complete JWT is recorded here.

## Passed

- Token getMe identifies `@complynx_test_bot`, ID 6087685431. No webhook or pending
  updates existed at initial connection. Telegram Web was already authenticated.
- Native Zitadel TLS certificate verification, real machine credentials,
  roleless human provisioning/delegation and the IAM v2 user API.
- Actual Loom auth sessions, duplicate create-run key returning the same run,
  real scheduler/agent loop and final result via its manual mailbox transport.
- Actual Telegram message → durable Shuttle task → automatic Zitadel identity →
  delegated Loom run → model mailbox response → real Telegram delivery.
  Task/run completed; one delivered outbox row. `Shuttle работает` has the stored
  Telegram entity `bold`, UTF-16 offset 0, length 16. The browser showed the reply.
- The next model request included the previous user message and assistant reply:
  actual Loom conversation history was carried into the subsequent run.
- The ingress behavioral HTTP-peer test covers a skipped group update followed
  by private acceptance returning 503, then success. Only the private update is
  forwarded twice, and the persisted offset advances after acceptance.
- Generated credentials and token file are ignored by Git; all published test
  ports bind loopback. No production or sibling stand was modified.

Mailbox responses for Telegram QA were authored by this Codex session. Their
Usage numbers are explicit scenario data, not measured model usage. The scripted
smoke response is deterministic. This is not an unattended Codex CLI backend.

## Failures retained

The first Telegram attempt returned access denied because the intermediate IAM
fixture used disabled TLS mode behind TLS termination. The second used a wrongly
configured extra `/v1` in Shuttle's Loom base URL, yielding an unmatched request.
Both fixture mistakes were corrected. The final stand uses native Zitadel TLS
and Shuttle's base URL without `/v1`. These failures are not counted as passes.

**Restart acceptance failed.** While task
`d3536e86-73d1-4ded-9329-422b9b8515f3` waited for a model response, Shuttle was
restarted. It retained run `b02ea0b8-8e4d-4afe-82f8-11082d8bcd01`, but the first
GET after readiness received HTTP 401 from Loom and Shuttle marked the task
failed. Loom later completed that same run. The original auth session subsequently
returned its completed result under a fresh actor credential without reauthorization.

This original failure is retained. Follow-up investigation established that Loom
Docker discovery temporarily removes the stopped Shuttle caller. The same auth
session/run returned 200 before stopping Shuttle, 401 while its caller was absent,
and 200 after discovery recovered, without reauthorization. The failed historical
task and its single error delivery remain in the test database.

## Authorization recovery fix and repeat acceptance

The fix waits on known Loom HTTP 401 responses with a durable five-minute deadline.
It preserves the existing run, session and operation keys. The deadline survives
Shuttle restarts; successful calls or a service-availability failure clear it.
Local revocation and HTTP 403 still deny access, and an unknown reauthorization
outcome is not replayed. See `shuttle-authorization-recovery.md` for the policy.

Repeat Telegram acceptance passed on Shuttle image
`sha256:918cfc42a28f1ddd67aa5c140233d89229574c6163ced81f807449a8edcc7d0f`.
Task `4e8ed98d-ae46-4ae4-912f-c525216970f9` retained Loom run
`9defa6aa-14eb-4ccb-a532-08c64d391da6` through two Shuttle recreations while
its discovery label was temporarily absent. It remained `awaiting_authorization`;
the persisted deadline stayed `2026-10-12T23:00:28.854293856Z` across the second
restart. Restoring the canonical discovery label resumed that same run and cleared
the deadline. After the manual mailbox response, both task and run completed.
Exactly one outbox delivery was stored, and Telegram Web showed
`Задача восстановлена без дублей`. The canonical Compose configuration is restored.

Six behavioral authorization-recovery tests cover restart recovery, expiry of the
persistent deadline, local revocation, lost reauthorization receipts, bootstrap
recovery and a service outage during the grace period. Full Linux race tests,
Windows behavioral tests, the linter and a fresh independent source review passed.
These results apply to the working-tree fix; the earlier PR CI does not cover it.

## Administrator tools and event catalogue

Actual Loom script execution discovered and called administration.settings for
an isolated fixture administrator. The fixture has shuttle.admin in both IAM
projects. The real Telegram user's account.me test completed with one delivery;
administration remained undiscoverable for that roleless user. Task
8fa1c88c-d80a-44b0-96bc-2266656e98ca retained run
690fbd45-71d0-4975-97c6-53e7b69d3092 and replied Инструменты аккаунта работают.
See shuttle-agent-administration.md and shuttle-events.md for contracts and
catalogue acceptance. These do not establish pass-based module eligibility.

## Simultaneous service outage and shutdown recovery

Test 8 stopped only this project's Loom and Zitadel, then recreated Shuttle.
Task f034cf06-2f07-4f06-8498-5bf0014df5fe remained awaiting_authorization during
the outage and retained original run 81d5715d-d85b-4597-a167-41a65f4039fe.
After both services recovered, the first attempt stalled: Loom preserved the run
as paused/shutdown, but Shuttle only polled. This was a Shuttle recovery defect.

The fixed build
sha256:90b1a04a540056ec387acf89469f4fac7610c10626814e7d31ff40de05c0441b
resumed the original run using a persisted operation key. That run and task
completed, with exactly one delivered outbox row. Telegram Web showed
Сервисы восстановились без дублей. No replacement run or task was created.
See shuttle-loom-resume.md for reconciliation behavior and the concurrent external
control limitation. The original failed attempt is retained above.

## Evidence and scope

Safe/private evidence and generated configuration remain in ignored
`.cache/shuttle-live-test/`; browser proofs include `telegram-pass.jpg`, `telegram-recovery-pass.jpg`,
`telegram-tools-pass.jpg` and `telegram-downtime-pass.jpg`. The restart
session probe records no credentials. The earlier messaging QA reports retain
their own recorded build identity. The acceptance results apply only to the specific slices and builds above.

No production deployment, cutover, shared zitadeltg linking, files or business
module acceptance was performed. The stand remains available for further QA;
use `deploy/shuttle/test/run.ps1 Stop` to stop only its services, preserving data.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
