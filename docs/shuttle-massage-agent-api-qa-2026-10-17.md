# Massage atomic Mini App store and agent API checkpoint

Date: 2026-10-17. Source at this checkpoint: schema 46 / manifest `mvp-46`.
The later [availability/native HTTP checkpoint](shuttle-massage-native-http-qa-2026-10-17.md) records current progress.
Owned runtime: schema 45 / manifest `mvp-44`, unchanged by this work.
Migration 46 and the new endpoints have not been installed on that runtime.

## Implemented source contract

Owner booking and cancellation share transaction-local workers. Public store
methods retain their earlier replay behavior: reading an existing receipt does
not commit a new effect; recovering a consumed proposal under a new operation ID
commits that new receipt against the same canonical booking/cancellation result.

Mini App store adapters check the current bearer, key generation, organization,
owner binding, expiry and revocation before preparation or receipt replay.
Operation, event, owner and session locks are acquired in that order. Session
validation and business writes use the same transaction/connection. IAM checks
belong before entry; no network call runs under those database locks. An expired
session must sign in again before recovering the original operation.

Operation receipts now retain their actual interface source independently of the
booking's creation source. For example, native recovery of an agent cancellation
has receipt source `miniapp`, while the original booking can still have source
`agent`. Replaying an existing operation does not rewrite its provenance.

The source manifest exposes `zinusya_massage.book`, `cancel`, `get`, `list` and
`operation` beside the existing schedule and schedule-only candidate methods.
Both writes have prepare/apply/status endpoints, exact-parameter confirmation,
nonzero matching operation/idempotency keys and a bound proposal ID. Preparation
holds no capacity. Apply rechecks all existing booking invariants. A changed body
or foreign proposal cannot reuse an approval. Historical owner get/list/cancel
need current binding, but no new assigned pass or enabled module.

Preparation descriptions show frozen service/specialist/party labels, price,
local start, treatment end, full occupancy end and event IANA zone. English and
Russian descriptions quote untrusted labels in a growing Markdown fence and fit
Loom's 4,000-byte description limit. They do not assert payment or a refund.

`get` returns current booking state; `operation` returns the immutable original
receipt with its kind, provenance and result. Book/cancel status does not accept a
receipt from the other phase. A missing receipt remains unknown, never approval
to repeat an uncertain effect. Stored `result.source` is the booking creation
source; receipt `source` identifies the operation interface.

List responses keep at most 20 records and fit a 60 KiB encoded result allowance,
leaving room within the advertised 64 KiB response limit. A shorter page resumes
after its last returned booking ID. When the whole page fits, its database cursor
is retained. Clients follow `next_after` until absent, even after a short page.

## Verification and retained failures

The atomic Mini App store gate passed 115 Linux/Go 1.27.2/PostgreSQL 17 race test
roots, with 211 passing test events, zero failures/skips and 951 frozen inputs.
Coverage includes both handoff directions, original replay after cancellation,
one-connection composition, logout/expiry/generation/foreign-owner/organization/
revocation denial before replay, session rechecks after an actual event-lock wait,
and rollback of booking/proposal consumption when receipt insertion fails.
Neighboring native order tests also passed. Independent correctness and quality
reviews found no actionable findings.

The first agent API gate passed 81 roots, but review found an uncovered response
limit defect. That green gate did not establish component completion. A first
12-record HTML-label probe did not demonstrate byte overflow; its unsplit-page
assertion failure is retained separately. The corrected 20-record UTF-8 probe
returned 98,988 HTTP response bytes, exceeding the advertised 65,536-byte bound.
The retained failure preceded the encoded-size pagination fix.

Final API QA passed all 82 selected race test roots, with 163 passing test events,
zero failures/skips and 957 frozen inputs. Real HTTP tests use isolated PostgreSQL
and mock IAM. They cover exact approvals, mixed store/HTTP handoffs, historical
status after cancellation, wrong-phase status, foreign-owner reads, admin-without-
pass denial and changed/missing approval rejection without creating a booking.
The 20-record history now uses two HTTP pages of 59,459 and 39,667 bytes and returns
every booking without duplicates or omissions. Independent final API correctness,
quality and pagination reviews found no actionable findings. Scoped configured
lint passed with zero issues; SQLc regeneration was byte-identical.

All disposable QA containers and their private volumes were removed, including
both failed reproduction attempts. No QA process was OOM-killed. The ten existing
Compose services retained their IDs/images/start times/restart counts; the
synthetic counted provider was not changed. The Go runners had a 2 GiB memory
limit and PostgreSQL 512 MiB, with swap disabled. Evidence remains in ignored
`.cache/shuttle-work/massage-miniapp-store-v1-qa/`, `massage-agent-api-v1-qa/`,
`massage-agent-page-red-v1-qa/`, `massage-agent-page-red-v2-qa/` and
`massage-agent-api-v2-qa/` directories.

## Remaining work at this checkpoint

This is component acceptance. Live reservation availability, Mini App HTTP
booking/history/status routes, native owner forms, pending-proposal/action context,
notifications and specialist timetable/exceptions remain open. The existing
`candidate_starts` endpoint still describes schedule-derived starts and must not
be called live availability. No massage workflow has passed real Telegram,
real-IAM/Loom or unattended-model acceptance. Browser-origin permission remains
pending; no browser workaround was used. Production and publication are unchanged.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
