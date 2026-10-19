# Isolated Telegram QA infrastructure: 2026-10-18

## Scope

Two independent projects, `zinusya-mock-qa-a` and `zinusya-mock-qa-b`, run the
reviewed schema48 / manifest `mvp-54` candidate with real PostgreSQL, Zitadel,
Loom, sandbox and file worker. Telegram is a local HTTP/TLS simulator. Model
responses use the synthetic counted mailbox provider; actual Loom tool dispatch
and application effects remain real. No production, real Telegram account, Loom
developer stand or primary `shuttle-live-test` service was changed.

The [operator guide](../deploy/shuttle/mock_tg/README.md) describes creation,
parallel ownership, messages, files, model replies, evidence and exact cleanup.
Each stand owns its database, network, volumes, IAM organization, credentials,
CA and mailbox. No real bot token is copied. Nine services have explicit limits;
actual memory was approximately 300 MiB per stand during this acceptance, with
no OOM events. This measurement is not a capacity guarantee.

## Accepted checks

- The mock and operator suite passes 18 focused tests: actual HTTP/TLS calls,
  callbacks and reply keyboards, edits, durable faults, files, session proxy,
  bounded journals and atomic mailbox replies. CI runs this suite.
- Two QA agents worked concurrently on different stands. Alice and Bob retain
  separate profiles and Mini App sessions. Native menus create no Loom run.
- Native profile updates, operation replay and stale-version rejection pass.
  A real Loom tool reads the native change; a second Loom operation changes the
  same profile and the native command and Mini App display the result. A second
  user remains unchanged, including a spoofed user query parameter. After the
  Loom/provider restart the same profiles and Mini App sessions remain valid;
  another actual Loom read sees the saved version without a business write.
- Replaying one Telegram update twice creates one durable task and one accepted
  outgoing message. A 429 with retry_after=4 creates no initial effect, then one
  accepted effect after 4.65 seconds.
- A disconnect after accepting a send leaves one stored mock message and a
  durable delivery_unknown task. It is not silently treated as delivered or
  automatically replayed. After the Loom/provider restart, its accepted effect and send-attempt counts
  were unchanged. Manual reconciliation remains outstanding by design.
- Cross-stand control credentials are rejected. A foreign user's attachment
  fetch fails without another Telegram download or artifact. The generic Loom
  error alone does not establish the exact HTTP authorization status.
- A third project, qa-factory, provisions and migrates from scratch with no manual
  repairs. All nine services pass startup checks and its native /menu task
  completes. Its containers, volumes and network were then removed; A/B runtime fingerprints stayed unchanged.

The Mini App checks use CA-verified HTTPS, signed synthetic initData, real bot
session bootstrap and user-scoped application endpoints. Production needs only
mini_app_origin; the optional mini_app_launch_url belongs only to test setups
that use the existing real-Telegram localhost redirect.

## Preserved failures and boundaries

Initial infrastructure attempts exposed an incorrectly encoded registration key,
UID/key permissions and Loom's loopback-only plaintext provider rule. Those
attempts remain failed evidence. Startup now checks every expected service and
bot, Loom and simulator readiness; the earlier bot-only false positive is not
counted as a passed stand. Startup stages guard uncertain IAM creation/migrations
against blind replay after an observation timeout.

The first file run fetched the exact 69-byte text fixture into a real Loom
artifact, but text_read failed while no file worker was configured. That run
finished with an honest limitation. A new run after adding the worker fetched a separate 70-byte UTF8 fixture and
text_read returned its exact contents, including Cyrillic, without truncation.
That task and its outgoing reply completed. The old failed result is retained;
PDF conversion and vision were not covered by this run.

The simulator accepts group and supergroup fixtures. Zinusya currently rejects
those incoming updates with HTTP400 and creates no group task or outgoing group
message. This infrastructure does not claim application group support.

Synthetic model counters and scripted responses do not prove model quality,
vision, billing or inference latency. Actual Telegram WebView launch, browser
permissions, client rendering and Telegram-verified user data still require a
small real-Telegram acceptance gate. Payment/registration, approval-gated actions,
orders, massage and specialist/admin permissions require their own native,
agent and mixed-path acceptance. This is infrastructure acceptance, not a full
product or production-readiness verdict.

## Evidence and next work

Ignored evidence is under .cache/shuttle-work/mock-tg-v1-qa/, with separate
agent-a and agent-b reports. Raw bootstrap proofs, tokens and private logs stay
ignored. A/B fixture state is retained for follow-up QA; stop idle stands to
release memory and destroy only exact owned projects when no longer needed.
The primary stand remains schema47 / mvp-51. Separate
[domain runtime QA](shuttle-isolated-domain-qa-2026-10-18.md) now accepts scoped
registration, assigned-pass reads, specialist authority and booking/cancellation
native/agent/mixed HTTP paths. Full qa-b stop/start preserves all task/outbox
counts and identical sends, including delivery_unknown. A/B are now stopped with
data retained. Return to the reviewed primary installation plan only after the
remaining source/acceptance work and a fresh verified backup.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
