# Catalog contract image activation — 2026-10-18

Status: PASS for an image-only update on the owned `shuttle-live-test` stand.
Runtime is schema48 / `mvp-55`. This is neither production acceptance nor a
complete live-controller rollback rehearsal.

The catalog description incorrectly said order proof/review was unimplemented.
The replacement points to `zinusya_order_payments`. The manifest revision changes
from 54 to 55. These are the only two changed release inputs among 856 frozen paths;
no schema, business logic, UI or dependency change is included.

## Source and build

Four existing HTTP behavioral test roots passed with Go1.27.2. Linux lint passed
with zero issues. An initial goimports failure from line endings was fixed with
gofmt and retained separately. Source review had no actionable findings.

The unmodified release Dockerfile built the full Linux image:
`sha256:6fe8c50c986e2b5f65c05f90f8294d31907bf03d7fa150d0f4f8d9237380ed7d`.
The dedicated builder used 2 GiB, no swap, 2 CPU and one build worker. It completed
without OOM. Its container and cache volume were removed; the image is retained.

## Actual activation

The reviewed operator freezes configuration bytes, exact Compose models, image
IDs, schema/history identity and companion containers before stopping ingress.
Only the bot image differs in the candidate model. The original image remains
pinned locally and has a validated Compose overlay. No migration, database
restore or automatic retry is performed.

The same poller stopped, then the original bot stopped normally in 0.641 seconds.
With both stopped, a read-only PostgreSQL snapshot captured 109 fingerprints of
all public tables, sequences, columns, indexes and constraints. One new bot
container started. Its complete runtime manifest matched the exact expected revision 55
manifest, with schema48/history and configuration unchanged.

All durable table rows, River job rows and sequences were unchanged. Only River
leader bookkeeping and the queue timestamp changed. Queue names, metadata,
creation time and pause state matched after excluding `updated_at` alone.
Eight Compose companions and the counted provider retained their identities,
start times and restart counts. The same poller resumed and completed a fresh
polling cycle. All ten Compose services were running without OOM.

Two existing claims were read through the reviewer API using a freshly acquired
IAM user token. Each is still pending/version1 for 25.68 BYN, with one submitted
order, two held soup units and no human review. There are zero unfinished bot
tasks. This update does not confirm money or repeat an owner submission.

## Review and retained observations

Independent operator review found a whole-table queue exception too broad.
The corrected operator requires exact River jobs and sequence preservation and
checks queue payload separately. Fresh review returned no actionable findings.
The index remained empty. Build-helper review was also clean.

The first ingress observation preceded its completed poll; observing the same
running container succeeded without another start. Read-only QA probes initially
used incorrect Python keywords, omitted the required API envelope, and assumed
`completed` rather than the actual `succeeded` API status. A recorder import
mistake also failed before action. Corrected probes did not replay business work.

Native evidence is retained under the ignored
`.cache/shuttle-work/catalog-image-swap-v1-qa/` directory: operator checkpoint,
109 before/after fingerprints, fresh-IAM responses, acceptance result and new
container baseline. Source tests and build evidence retain their separate gates.

Next: pure-agent and agent-to-native reviewer decisions on the two existing
pending claims, with fresh exact consent. Fault/outage gates, unattended model
behavior and complete live-controller rollback remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk

Subsequent [reviewer dialogue and cancellation QA](shuttle-order-reviewer-dialogue-qa-2026-10-18.md) confirms the first claim through the agent
and cancels only the second review intent after an incomplete native attempt.
The second claim remains pending. This does not change this report's historical
acceptance state or broaden its scope.
