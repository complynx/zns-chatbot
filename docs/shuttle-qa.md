# Behavioral acceptance

Use the specification and domain contracts as the checklist. Record each requirement against the exact source, build and dependency versions used for that observation. Source review, developer tests, controlled orchestration, actual model behavior, real Telegram behavior and release readiness are separate evidence levels.

## Interface matrix

| Workflow family | Native only | Agent only | Native to agent | Agent to native |
| --- | --- | --- | --- | --- |
| Profile and legal registration details | Required | Required | Required | Required |
| Registration, pairs and pass assignment | Required | Required | Required | Required |
| Payment evidence, human review and top-up | Required | Required | Required | Required |
| Service orders and meals | Required | Required | Required | Required |
| Massage participant and specialist workflows | Required | Required | Required | Required |
| Avatar drafts, generation and delivery | Required | Required | Required | Required |
| Knowledge proposals, review and publication | Required | Required | Required | Required |

These cells define coverage; they do not assert a completed test run. Verify native independence with Loom unavailable. For agent paths, observe actual tool dispatch and persisted outcomes. A direct test-client mutation or seeded final business record cannot stand in for the user workflow.

## Required boundaries

Use fresh synthetic participants and an owned disposable event. Setup may configure authorities and catalogs. User registrations, choices, evidence, consent and resulting effects must come through the interface being accepted.

For each affected workflow, cover the happy path and relevant critical boundaries: stale versions, changed prices/deadlines, duplicate input, response loss, current authority loss, wrong owner/event, expired sessions, cancellation and restart. Exercise both handoffs while the original question or preparation remains pending. Preserve the original operation/proposal and immutable result. Reopening a completed link must read the original result without another effect.

Check persistent domain rows, original receipts, draft versions, outbox state and delivery recipient. Payment evidence must remain pending until authorized human review. An agent statement does not establish a bank fact or user consent.

File checks include owner isolation, explicit source loss versus transient failure, bounded reads and external-source unavailability. Audio checks compare complete transcript bytes across pages, source collection, process restart and reuse by another run; current disclosure authority must still apply. A controlled ASR stub proves the integration contract, not speech quality.

## Execution and custody

### Local ingress recovery

`deploy/shuttle/test/receiver.py` is an owned-stand receiver with durable raw
batches, per-update receipts and a persisted acknowledgment cursor. It starts
HELD and verifies bot identity and webhook ownership before polling. Temporary
network failures, HTTP 408/429 and selected 5xx responses retry with bounded
backoff at the same cursor. Telegram `RetryAfter` is respected. Authentication,
consumer conflicts, certificate failures, invalid state and storage failures
remain held with content-free diagnostics.

A retained batch is resumed before polling, including updates without individual
receipts. Capture-only records are distinct from pending forwarding. Exact
webhook submissions may repeat only against Shuttle's durable bot/update inbox
deduplication; paid requests and uncertain message sends have no such retry
permission. Changed webhook targets or ambiguous legacy receipts need inspection.
The cursor advances only after classification or durable inbox acknowledgment and
successful local persistence. Test response loss after inbox acceptance, process
restart, storage failure and withdrawal of forwarding authority.

These fixtures prove local ingress contracts. They do not prove external model
quality or production telemetry ingestion.

| Operation | Recover automatically | Keep held |
| --- | --- | --- |
| Telegram reads | Transport, 408/429, selected 5xx; same durable cursor | Wrong bot, 401/403/409, invalid certificate |
| Shuttle inbox | Repeat exact retained update against durable dedup | Changed target, invalid ACK, legacy ambiguity |
| Cursor storage | Reuse last successfully persisted cursor | Disk or malformed-state failure |
| Descriptor reads | Same generation/run/artifacts; durable backoff | Invalid result, auth failure, unknown mutation |
| Telegram delivery | Existing known-safe outbox retry policy | Uncertain send outcome |
| Paid provider | Existing completed-response receipt | Reserved/orphaned/unknown paid dispatch |

Review a one-shot fixture before running it. Pin immutable inputs, constrain effects and external-call budgets, and persist intent before dispatch. A missing or uncertain receipt is not permission to rerun. Retain the failed original attempt; use a separately reviewed observation when final state can be proved without mutation.

After the observed flow, archive and fully decode owned databases before removing only identified fixture resources. Verify remaining resource counts independently. A successful flow with cleanup still pending is not a cleanup PASS. Keep private payloads and credentials out of public summaries.

Use focused tests for the changed contract and the required CI gates. Reuse earlier evidence only with explicit source/behavior bounds; do not imply a full matrix rerun. The [integration runner](shuttle-integration-batches.md) bounds the complete PostgreSQL suite. The [registration procedure](shuttle-registration-pass-parity-plan.md) gives one complete business journey.

Execution reports, screenshots, receipts and environment inventories belong in local evidence storage. Maintained documentation records contracts and reusable procedures. Publication and deployment remain separately authorized actions.
