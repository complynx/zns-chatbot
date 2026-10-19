# Agent and mixed registration runtime QA — 2026-10-19

All three remaining required-fields to solo leader waiting-list paths pass on an
isolated real-Zitadel/Loom/mock-Telegram stand. Combined with the separate
[native-only gate](shuttle-registration-native-runtime-qa-2026-10-19.md), this
accepts four interface combinations for this bounded registration workflow.
It does not accept pass assignment, payment or unattended model comprehension.

## Participants and actual tools

Four fresh synthetic users were bootstrapped into real IAM. Fixture SQL configured
one event and its requirements; it inserted no legal fields, registrations or
passes. Three users exercised these paths; one remained unused. The existing
primary image was used, without changing product code or schema.

- **Agent only:** the user explicitly supplies both legal values and asks to join
  as leader. Real Loom reads readiness/status, calls `begin_request`,
  `set_legal_name`, `set_passport_number`, refreshes readiness and calls
  `zinusya_registrations.join`. One waiting-list registration results.
- **Native to agent:** `/register` and its role button create the expectation;
  the actual form saves only legal name. The next active Loom request already
  contains that expectation ID in untrusted context. Tools refresh status and
  readiness, see version1/name present/passport missing, save the supplied
  passport and join as leader. No second name write or expectation is created.
- **Agent to native:** Loom calls `begin_request` and saves only legal name,
  respecting the user's request to continue later. It creates no registration.
  The bot is stopped gracefully and restarted in the same container: stop takes
  0.625 seconds, exit0, no OOM. The expectation and deadline are identical.
  A fresh actual form sees the saved name and saves only passport. It still does
  not register. `/register` and a delivered Leader button explicitly register
  that same user. This native continuation creates no extra Loom run.

The browser contexts use signed mock initData and a mock Telegram SDK, with a
context-local TLS exception. Both screenshots were saved; the agent-to-native
form screenshot was visually inspected. This is separate from real Telegram Web.
Each browser performs exactly one field write and signs out afterward.

The controlled provider supplies model tool decisions, but real Loom executes
scripts and writes through authenticated bot APIs. Direct QA API writes are not
substituted for the agent. Three user messages create three completed Loom runs;
no summary/compaction step was needed. This proves orchestration and contracts,
not an unattended model's interpretation or measured token cost.

## Database and delivery readback

There are exactly three leader waiting-list registrations, three completed
expectations, six legal-detail mutation receipts and three registration receipts.
Each owner has legal-details version2 with both presence flags. Each expectation
retains its original ID and expiry instant. There are zero passes. All bot tasks,
outbox deliveries and Loom runs have completed. Each registration matches its
actual tool or callback result, rather than a SQL-created fixture row.

The first final observer incorrectly compared timestamp strings: API `Z` and
PostgreSQL `+00:00` encode the same instant. The failed check is retained in
`acceptance-v1-failure.json`. The corrected read-only observer compares timezone
aware instants. No user message, tool mutation or registration was replayed, and
no product change was made for this observer issue.

All three databases were dumped and the archives validated before cleanup.
All nine exact owned services stopped without OOM; the bot and PostgreSQL exited0.
The stand's containers, volumes and network were removed. The original primary
remains unchanged: 11 services, schema59, manifest `zinusya`/`mvp-68`, 134 Loom runs
and preserved domain data. Evidence remains under ignored
`.cache/shuttle-work/registration-mixed-runtime-v1-qa/`.

Next: native and agent authority paths for pass assignment, then participant
payment evidence, human review and top-up. Full profile-name parity, broader
business workflow coverage, actual Telegram smoke acceptance and unattended
model acceptance remain separate gates. See the
[workflow matrix](shuttle-registration-pass-parity-plan.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
